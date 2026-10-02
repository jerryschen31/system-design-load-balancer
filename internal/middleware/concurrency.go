package middleware

import (
	"container/list"
	"context"
	"net/http"
	"sync"
	"time"
)

// acquireResult reports how a request's attempt to get a concurrency slot
// was resolved -- there are four distinct outcomes, and the handler needs
// to react differently to each (proceed, or one of three different ways of
// not proceeding).
type acquireResult int

const (
	acquireGranted acquireResult = iota
	acquireQueueFull
	acquireTimedOut
	acquireClientGaveUp
)

// waiter is one request parked in the FIFO queue. ready is closed exactly
// once, by release(), to wake this specific waiter and only this waiter --
// closing (rather than sending a value) means the wake-up is visible
// immediately to a <-w.ready read even if nobody was actively selecting on
// it at the exact instant of the close.
type waiter struct {
	ready chan struct{}
}

// limiter caps how many requests may hold a slot at once, plus an
// optional bounded wait queue for requests that show up once every slot
// is taken. This is a semaphore: a shared counter that tracks how many
// slots are currently in use, where "acquire" only succeeds if a slot is
// free (and otherwise blocks or fails, depending on the caller), and
// "release" gives a slot back for someone else to use. It's the standard
// tool for "at most N of these may happen at once" -- size is that N
// (the hard concurrency ceiling, like phase 4a's plain semaphore before
// the queue existed); queueCap bounds how many additional requests may
// wait for a slot to free up, so an overloaded backend can never cause
// unbounded goroutines/connections to pile up here either -- the same
// reasoning that motivated size in the first place, one layer up. "FIFO"
// means the queue is strictly first-come-first-served: see release()
// below for how that's enforced.
type limiter struct {
	mu       sync.Mutex
	cur      int
	size     int
	queueCap int
	waiters  *list.List // of *waiter, oldest (longest-waiting) at Front()
}

func newLimiter(size, queueCap int) *limiter {
	return &limiter{size: size, queueCap: queueCap, waiters: list.New()}
}

// acquire tries to get a slot, waiting in the FIFO queue (up to
// queueWaitTimeout, or until ctx is done) if none are immediately free.
func (l *limiter) acquire(ctx context.Context, queueWaitTimeout time.Duration) acquireResult {
	l.mu.Lock()
	if l.cur < l.size {
		l.cur++
		l.mu.Unlock()
		return acquireGranted
	}
	if l.waiters.Len() >= l.queueCap {
		l.mu.Unlock()
		return acquireQueueFull
	}
	w := &waiter{ready: make(chan struct{})}
	elem := l.waiters.PushBack(w)
	l.mu.Unlock()

	timer := time.NewTimer(queueWaitTimeout)
	defer timer.Stop()

	select {
	case <-w.ready:
		// release() already handed us the slot directly -- cur was never
		// decremented for this handoff (see release()), so it's already
		// accounted for; nothing left to do but use it.
		return acquireGranted
	case <-timer.C:
		return l.giveUp(elem, w, acquireTimedOut)
	case <-ctx.Done():
		return l.giveUp(elem, w, acquireClientGaveUp)
	}
}

// giveUp handles a waiter that has decided to stop waiting (timed out, or
// the client disconnected). The subtle part: release() may have already
// granted this exact waiter the slot (closed w.ready) in the brief window
// between that decision and this function acquiring the lock -- both are
// checked under the same mutex that release() uses to grant slots, so
// exactly one outcome is possible, never a lost or double-granted slot.
func (l *limiter) giveUp(elem *list.Element, w *waiter, reason acquireResult) acquireResult {
	l.mu.Lock()
	select {
	case <-w.ready:
		// We were granted the slot just before giving up. We're not going
		// to use it -- pass it along to whoever's next (or actually free
		// it, via release()'s own logic) rather than leaking it forever.
		l.mu.Unlock()
		l.release()
	default:
		// Not granted -- remove ourselves so a future release() never
		// tries to wake a waiter nobody is listening for anymore.
		l.waiters.Remove(elem)
		l.mu.Unlock()
	}
	return reason
}

// release frees a slot. If anyone is waiting, the slot is handed directly
// to the longest-waiting one (FIFO) without ever being "let go" for a new
// arrival to grab first -- that direct handoff is what makes this fair
// instead of best-effort.
//
// close(w.ready) happens while l.mu is still held, deliberately -- not
// after unlocking. giveUp() below re-checks w.ready under this same lock
// to decide whether a waiter that's timing out was already granted the
// slot. If the close happened after unlocking, there would be a window
// where release() has committed this slot to w (removed it from the
// list, so no one else will ever be offered it) but hasn't signaled w
// yet -- a giveUp() call landing in exactly that window would see "not
// signaled" and walk away, permanently leaking the slot, since release()
// already considers it spoken for. Keeping both steps (remove from the
// list, signal the waiter) inside one critical section closes that
// window: by the time giveUp() is even allowed to check, the answer is
// already final.
func (l *limiter) release() {
	l.mu.Lock()
	if front := l.waiters.Front(); front != nil {
		w := l.waiters.Remove(front).(*waiter)
		close(w.ready)
		l.mu.Unlock()
		return
	}
	l.cur--
	l.mu.Unlock()
}

// queueLen reports how many requests are currently waiting. Unexported --
// it exists purely so tests can deterministically observe "this waiter has
// actually joined the queue" instead of guessing via a sleep; it is not
// part of the public API.
func (l *limiter) queueLen() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.waiters.Len()
}

// MaxConcurrent returns middleware that allows at most n requests to be in
// flight through the wrapped handler at once. A request arriving while all
// n slots are held waits in a bounded, strictly FIFO queue (capacity
// queueCapacity, each wait capped at queueWaitTimeout) for a slot to free
// up, rather than being rejected immediately -- phase 4a's plain
// fail-fast behavior is queueCapacity <= 0, still fully supported (and
// still what MaxConcurrent(n, 0, 0) gives you).
//
// Three distinct ways a request can fail to get a slot, each reported
// differently to the client: the queue itself was already full (503,
// immediately); the request waited but queueWaitTimeout elapsed first
// (503, after waiting); or the client disconnected while queued, in which
// case there is nothing left to respond to at all.
//
// This is the same fair-semaphore shape as golang.org/x/sync/semaphore's
// Acquire, applied to plain net/http middleware instead of that package's
// weighted-token API.
func MaxConcurrent(n int, queueCapacity int, queueWaitTimeout time.Duration) func(http.Handler) http.Handler {
	if n <= 0 {
		panic("middleware: MaxConcurrent requires a positive n")
	}
	if queueCapacity < 0 {
		panic("middleware: MaxConcurrent requires a non-negative queueCapacity")
	}
	if queueCapacity > 0 && queueWaitTimeout <= 0 {
		panic("middleware: MaxConcurrent requires a positive queueWaitTimeout when queueCapacity > 0")
	}
	l := newLimiter(n, queueCapacity)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch l.acquire(r.Context(), queueWaitTimeout) {
			case acquireGranted:
				defer l.release()
				next.ServeHTTP(w, r)
			case acquireQueueFull:
				http.Error(w, "too many concurrent requests", http.StatusServiceUnavailable)
			case acquireTimedOut:
				http.Error(w, "timed out waiting for a free slot", http.StatusServiceUnavailable)
			case acquireClientGaveUp:
				// The client is already gone; there is no one to write a
				// response to. Same posture as echobackend's /sleep
				// handler on its own cancellation path.
			}
		})
	}
}
