package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// waitForQueueLen busy-polls l's queue length until it reaches want or a
// generous deadline passes. This is a deliberate, bounded poll of real
// authoritative state (not a blind sleep-and-hope): there is no channel or
// callback the limiter exposes for "a waiter just joined the queue", so
// polling queueLen() -- an in-process, mutex-guarded read, not a network
// call -- is the simplest way to know deterministically that a previous
// waiter has actually been enqueued before the test proceeds to enqueue
// the next one, which matters for proving FIFO order below.
func waitForQueueLen(t *testing.T, l *limiter, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if l.queueLen() == want {
			return
		}
	}
	t.Fatalf("queue length never reached %d (last observed %d)", want, l.queueLen())
}

// TestMaxConcurrentAllowsExactlyNInFlight is the phase 4a boundary test,
// unchanged in spirit: with queueCapacity=0 (no queue), a request beyond n
// concurrent is rejected immediately, not queued.
func TestMaxConcurrentAllowsExactlyNInFlight(t *testing.T) {
	const n = 3
	arrived := make(chan struct{}, n)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(MaxConcurrent(n, 0, 0)(inner))
	defer srv.Close()
	t.Logf("input: MaxConcurrent(%d, 0, 0) wrapping a handler that blocks until released", n)

	var admittedWg sync.WaitGroup
	admittedStatuses := make([]int, n)
	for i := 0; i < n; i++ {
		admittedWg.Add(1)
		go func(i int) {
			defer admittedWg.Done()
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Errorf("admitted request %d failed: %v", i, err)
				return
			}
			defer resp.Body.Close()
			admittedStatuses[i] = resp.StatusCode
		}(i)
	}
	for i := 0; i < n; i++ {
		<-arrived
	}
	t.Logf("step: %d requests confirmed in flight (holding every semaphore slot)", n)

	const numRejected = 2
	var rejectedWg sync.WaitGroup
	rejectedStatuses := make([]int, numRejected)
	rejectedWg.Add(numRejected)
	for i := 0; i < numRejected; i++ {
		go func(i int) {
			defer rejectedWg.Done()
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Errorf("rejected request %d failed: %v", i, err)
				return
			}
			defer resp.Body.Close()
			rejectedStatuses[i] = resp.StatusCode
		}(i)
	}
	rejectedWg.Wait() // hangs until test timeout if these were queued instead of rejected
	t.Logf("step: %d additional requests while at capacity -> statuses %v", numRejected, rejectedStatuses)
	for i, status := range rejectedStatuses {
		if status != http.StatusServiceUnavailable {
			t.Errorf("rejected request %d got status %d, want %d", i, status, http.StatusServiceUnavailable)
		}
	}

	close(release)
	admittedWg.Wait()
	t.Logf("output: the %d originally-admitted requests all completed with statuses %v", n, admittedStatuses)
	for i, status := range admittedStatuses {
		if status != http.StatusOK {
			t.Errorf("admitted request %d got status %d, want %d", i, status, http.StatusOK)
		}
	}
}

// TestLimiterAcquireIsFIFO tests the queueing algorithm directly (not
// through HTTP) so ordering can be proven deterministically: 1 slot, 3
// waiters enqueued one at a time (each confirmed actually enqueued via
// waitForQueueLen before the next is started), then the slot released 3
// times in a row. The order requests actually acquire the slot must match
// the order they joined the queue, not the order goroutines happen to be
// scheduled.
func TestLimiterAcquireIsFIFO(t *testing.T) {
	l := newLimiter(1, 3)
	if res := l.acquire(context.Background(), time.Second); res != acquireGranted {
		t.Fatalf("initial acquire: got %v, want acquireGranted", res)
	}
	t.Log("input: 1 slot (held), 3 waiters joining the queue strictly in order 0, 1, 2")

	admitted := make(chan int, 3)
	for i := 0; i < 3; i++ {
		go func(id int) {
			res := l.acquire(context.Background(), 2*time.Second)
			if res != acquireGranted {
				t.Errorf("waiter %d: got %v, want acquireGranted", id, res)
				return
			}
			admitted <- id
		}(i)
		waitForQueueLen(t, l, i+1)
	}
	t.Logf("step: all 3 waiters confirmed enqueued (queue length reached 3)")

	// Release one slot at a time, reading admitted before issuing the
	// next release. This matters: closing a waiter's ready channel makes
	// it *grantable*, but Go's scheduler gives no guarantee about the
	// order in which separately-blocked goroutines actually resume and
	// run once unblocked -- firing all 3 releases back-to-back and then
	// comparing a freely-raced append order (an earlier version of this
	// test did exactly that) tests scheduler luck, not the algorithm.
	// Waiting for each admission before the next release works because,
	// at each point, only the single most-recently-woken waiter's
	// goroutine is capable of sending on admitted at all -- so the value
	// received is unambiguous, not a race.
	var admissionOrder []int
	for i := 0; i < 3; i++ {
		l.release()
		admissionOrder = append(admissionOrder, <-admitted)
	}

	t.Logf("output: admission order was %v, want [0 1 2]", admissionOrder)
	want := []int{0, 1, 2}
	for i := range want {
		if admissionOrder[i] != want[i] {
			t.Errorf("admission order %v does not match FIFO join order %v", admissionOrder, want)
			break
		}
	}
}

// TestLimiterGiveUpOnClientCancelFreesQueueSlot proves the giveUp path:
// a waiter whose context is cancelled while queued (client disconnected)
// stops waiting and its queue slot becomes available again for someone
// else, rather than being held forever.
func TestLimiterGiveUpOnClientCancelFreesQueueSlot(t *testing.T) {
	l := newLimiter(1, 1)
	if res := l.acquire(context.Background(), time.Second); res != acquireGranted {
		t.Fatalf("initial acquire: got %v, want acquireGranted", res)
	}
	t.Log("input: 1 slot (held), queue capacity 1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan acquireResult, 1)
	go func() {
		done <- l.acquire(ctx, 10*time.Second) // long timeout: cancellation should win, not this
	}()
	waitForQueueLen(t, l, 1)
	t.Log("step: waiter confirmed enqueued")

	cancel()
	res := <-done
	t.Logf("output: cancelled waiter's acquire returned %v, want acquireClientGaveUp", res)
	if res != acquireClientGaveUp {
		t.Errorf("got %v, want acquireClientGaveUp", res)
	}
	if got := l.queueLen(); got != 0 {
		t.Errorf("queue length after cancellation is %d, want 0 -- the given-up waiter's slot was not freed", got)
	}
}

// TestMaxConcurrentQueueAdmitsAfterWait is the HTTP-level proof of the
// success path: a request arriving while all slots are held, but with a
// slot freeing up before queueWaitTimeout elapses, is admitted (200)
// rather than rejected.
func TestMaxConcurrentQueueAdmitsAfterWait(t *testing.T) {
	const n = 1
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(MaxConcurrent(n, 1, 2*time.Second)(inner))
	defer srv.Close()
	t.Log("input: 1 slot (held), queue capacity 1, queueWaitTimeout 2s; a second request arrives while the first is in flight")

	holderDone := make(chan int, 1)
	go func() {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Errorf("holder request failed: %v", err)
			holderDone <- -1
			return
		}
		defer resp.Body.Close()
		holderDone <- resp.StatusCode
	}()
	<-arrived
	t.Log("step: holder confirmed in flight, holding the only slot")

	queuedDone := make(chan int, 1)
	go func() {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Errorf("queued request failed: %v", err)
			queuedDone <- -1
			return
		}
		defer resp.Body.Close()
		queuedDone <- resp.StatusCode
	}()

	// Give the queued request a moment to actually reach the middleware
	// and join the queue before releasing the holder -- otherwise we might
	// release before it's even queued, which would just test the ordinary
	// immediate-grant path instead of the queue+wakeup path this test is
	// for. A short, generous sleep here is acceptable: it only widens the
	// window before we release, it isn't used to detect an event.
	time.Sleep(50 * time.Millisecond)
	close(release)

	holderStatus := <-holderDone
	queuedStatus := <-queuedDone
	t.Logf("output: holder status=%d, queued (waited then admitted) status=%d", holderStatus, queuedStatus)
	if holderStatus != http.StatusOK {
		t.Errorf("holder got status %d, want %d", holderStatus, http.StatusOK)
	}
	if queuedStatus != http.StatusOK {
		t.Errorf("queued request got status %d, want %d -- it should have been admitted once the holder's slot freed up", queuedStatus, http.StatusOK)
	}
}

// TestMaxConcurrentQueueTimesOut is the HTTP-level proof of the failure
// path: a request that waits the full queueWaitTimeout without a slot
// freeing up gets a distinct 503 body, not the immediate-rejection one.
func TestMaxConcurrentQueueTimesOut(t *testing.T) {
	const n = 1
	const queueWaitTimeout = 100 * time.Millisecond
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(MaxConcurrent(n, 1, queueWaitTimeout)(inner))
	defer srv.Close()
	defer close(release) // let the holder finish so the test can exit cleanly
	t.Logf("input: 1 slot held indefinitely, queueWaitTimeout=%s, a second request that will never be released in time", queueWaitTimeout)

	go http.Get(srv.URL) // holder; response ignored, cleaned up via defer close(release) + server.Close()
	<-arrived
	t.Log("step: holder confirmed in flight, holding the only slot")

	start := time.Now()
	resp, err := http.Get(srv.URL)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("queued request failed: %v", err)
	}
	defer resp.Body.Close()
	body := make([]byte, 128)
	n2, _ := resp.Body.Read(body)

	t.Logf("output: status=%d body=%q elapsed=%s", resp.StatusCode, string(body[:n2]), elapsed)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	const wantBody = "timed out waiting for a free slot\n"
	if string(body[:n2]) != wantBody {
		t.Errorf("got body %q, want %q", string(body[:n2]), wantBody)
	}
	if elapsed < queueWaitTimeout {
		t.Errorf("request returned after %s, want at least queueWaitTimeout (%s)", elapsed, queueWaitTimeout)
	}
}

// TestMaxConcurrentQueueFullRejectsImmediately proves the third outcome:
// once both the slots and the queue itself are full, a further request is
// rejected immediately, with the original (not the timeout) 503 body, and
// without waiting queueWaitTimeout at all.
func TestMaxConcurrentQueueFullRejectsImmediately(t *testing.T) {
	const n = 1
	const queueCapacity = 1
	const queueWaitTimeout = 5 * time.Second // deliberately long; overflow must not wait any of it
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(MaxConcurrent(n, queueCapacity, queueWaitTimeout)(inner))
	defer srv.Close()
	defer close(release)
	t.Logf("input: 1 slot held, queue capacity 1 also held, a 3rd (overflow) request arrives")

	go func() {
		resp, err := http.Get(srv.URL) // occupies the slot
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-arrived
	t.Log("step: 1st request confirmed holding the only slot")

	go func() {
		resp, err := http.Get(srv.URL) // occupies the only queue slot
		if err == nil {
			resp.Body.Close()
		}
	}()
	// Give the 2nd request a generous, fixed window to actually reach the
	// middleware and join the queue before firing the overflow request
	// below -- this only widens the window before the real assertion, it
	// isn't used to detect an event (loopback round-trips here are
	// microseconds, so 50ms is enormous headroom).
	time.Sleep(50 * time.Millisecond)
	t.Log("step: 2nd request given time to join the only queue slot")

	start := time.Now()
	resp, err := http.Get(srv.URL)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("overflow request failed: %v", err)
	}
	defer resp.Body.Close()
	body := make([]byte, 128)
	n2, _ := resp.Body.Read(body)

	t.Logf("output: overflow request -> status=%d body=%q elapsed=%s", resp.StatusCode, string(body[:n2]), elapsed)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	const wantBody = "too many concurrent requests\n"
	if string(body[:n2]) != wantBody {
		t.Errorf("got body %q, want %q", string(body[:n2]), wantBody)
	}
	if elapsed >= queueWaitTimeout {
		t.Errorf("overflow request took %s (>= queueWaitTimeout %s); it should have been rejected immediately, never waiting", elapsed, queueWaitTimeout)
	}
}

func TestMaxConcurrentPanicsOnNonPositiveN(t *testing.T) {
	t.Log("input: MaxConcurrent(0, 0, 0)")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected MaxConcurrent(0, 0, 0) to panic, so a misconfiguration surfaces at construction instead of silently blocking all traffic")
		}
		t.Logf("output: panicked as expected: %v", r)
	}()
	MaxConcurrent(0, 0, 0)
}

func TestMaxConcurrentPanicsOnNegativeQueueCapacity(t *testing.T) {
	t.Log("input: MaxConcurrent(1, -1, 0)")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a negative queueCapacity to panic")
		}
		t.Logf("output: panicked as expected: %v", r)
	}()
	MaxConcurrent(1, -1, 0)
}

func TestMaxConcurrentPanicsOnQueueWithoutWaitTimeout(t *testing.T) {
	t.Log("input: MaxConcurrent(1, 5, 0) -- a real queue capacity but no positive wait timeout")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected queueCapacity > 0 with queueWaitTimeout <= 0 to panic")
		}
		t.Logf("output: panicked as expected: %v", r)
	}()
	MaxConcurrent(1, 5, 0)
}
