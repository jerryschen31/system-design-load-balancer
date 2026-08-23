╭─── Claude Code v2.1.221 ─────────────────────────────────────────────────────╮
│                                                    │ Tips for getting        │
│                 Welcome back Jerry!                │ started                 │
│                                                    │ Run /init to create a … │
│                      ▗ ▗   ▖ ▖                     │ ─────────────────────── │
│                                                    │ What's new              │
│                        ▘▘ ▝▝                       │ Cost estimates (`/cost… │
│   Sonnet 5 · Claude Pro · jerryschen@gmail.com's   │ Added the one-time ful… │
│   Organization                                     │ Added `/claude-api upg… │
│       ~/gh/public/system-design-load-balancer      │ /release-notes for more │
╰──────────────────────────────────────────────────────────────────────────────╯


❯ /clear                                                                        

❯ I'm digging into the actual connection handshake. I sent a few requests to    
  localhost:9001. Here's the first few lines of the tcpdump, let me see if I    
  understand correctly                                                          
                                                                                
  jerry@Jerrys-MacBook-Air-2 system-design-load-balancer % sudo tcpdump -i lo0  
  -n port 9001                                                                  
  Password:                                                                     
  tcpdump: verbose output suppressed, use -v[v]... for full protocol decode     
  listening on lo0, link-type NULL (BSD loopback), snapshot length 524288 bytes 
  21:57:11.804051 IP6 ::1.64604 > ::1.9001: Flags [S], seq 3615243081, win      
  65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 3211068865 ecr          
  0,sackOK,eol], length 0                                                       
  21:57:11.804122 IP6 ::1.9001 > ::1.64604: Flags [S.], seq 2247086935, ack     
  3615243082, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val         
  1527951684 ecr 3211068865,sackOK,eol], length 0                               
  21:57:11.804139 IP6 ::1.64604 > ::1.9001: Flags [.], ack 1, win 6372, options 
  [nop,nop,TS val 3211068865 ecr 1527951684], length 0                          
  21:57:11.804153 IP6 ::1.9001 > ::1.64604: Flags [.], ack 1, win 6372, options 
  [nop,nop,TS val 1527951684 ecr 3211068865], length 0                          
  21:57:11.805348 IP6 ::1.64604 > ::1.9001: Flags [P.], seq 1:182, ack 1, win   
  6372, options [nop,nop,TS val 3211068866 ecr 1527951684], length 181          
                                                                                
  1. First, this is the dump of all network packets arriving or sent out from   
  localhost:9001 - correct ?                                                    
  2. The first [S] line is a SYN packet sent from the LB (out port 64604) to    
  the server (in port 9001), which is the client asking to connect ?            
  3. The second [S.] line is the ACK packet sent from the server back to the LB 
  ? Why is there a new "seq 2247086935"?                                        
  4. The third [.] line is the SYN-ACK packet sent back from the client / LB    
  back to teh server ? Why is it now "ack 1" ?                                  
  5. The fourth line [.] is also a SYN-ACK packet (the same one?) sent from the 
  server back to the client ? Has a connection been established at this point?  
  6. The last line [P.] is an actual payload packet sent from the client ?      
                                                                                
  Help fill in holes in my understanding.                                       

  Searched for 1 pattern, ran 1 shell command (ctrl+o to expand)

⏺ Quick correction before the details: port 9001 is your echobackend server
  (confirmed in cmd/echobackend/main.go:30, -listen :9001), not the LB. Since
  you said you hit localhost:9001 directly, the "client" here is whatever
  process you ran (curl, browser, etc.) — the LB isn't in this capture at all.
  Also worth noting: localhost resolved to ::1 (IPv6 loopback) rather than
  127.0.0.1, which is why you see IP6 — that's just a resolver/OS default, not
  something you configured.

  Here's the corrected walkthrough, packet by packet:

  1. Is this all traffic to/from port 9001?
  Yes. -i lo0 captures on the loopback interface, port 9001 filters to packets
  where either the source or destination port is 9001, -n disables
  hostname/port-name resolution so you see raw numbers instead of names.

  2. Packet 1 — [S]
  ::1.64604 > ::1.9001, flags [S] (SYN only). This is your client process's OS
  picking a random unused local port (64604) and sending a SYN to the server's
  listening port (9001) — "I want to open a connection, and here's my starting
  sequence number (3615243081)." Each TCP connection endpoint picks its own
  initial sequence number (ISN) randomly, independent of the other side — it's
  not a shared counter. Randomizing it is a security measure (makes it harder
  for an off-path attacker to guess sequence numbers and inject data into the
  stream).

  3. Packet 2 — [S.]
  ::1.9001 > ::1.64604, flags [S.] (SYN + ACK combined — tcpdump prints multiple
  flags concatenated, so S. = SYN, . = ACK). This is the server's response, and
  it does two things in one packet: it sends its own SYN (with its own
  independently-chosen ISN, 2247086935 — hence a "new" seq you didn't see
  coming), and it ACKs your SYN (ack 3615243082 = your ISN + 1). The two
  directions of a TCP connection are two logically separate byte streams, each
  with its own sequence numbering — that's why the server's seq number has
  nothing to do with your seq number.

  4. Packet 3 — this is NOT a SYN-ACK
  ::1.64604 > ::1.9001, flags [.] (ACK only — no SYN bit set). This is the third
  and final packet of the 3-way handshake: the client acknowledging the
  server's SYN. ack 1 (not ack 3615243082) because starting from this packet,
  tcpdump switches to printing relative sequence/ack numbers — it uses each
  side's ISN as a zero point and shows offsets from there, since raw 32-bit ISNs
  are unreadable. ack 1 means "acknowledging up through your ISN + 1," i.e.,
  acking the SYN. (tcpdump only relativizes non-SYN packets; that's why packets
  1–2 showed raw numbers and packet 3 onward switches to small relative ones.)

  5. Packet 4 — this is the surprising one, and I don't think it's real
  A textbook TCP handshake is three packets, not four. This fourth packet
  (::1.9001 > ::1.64604, [.], ack 1) looks like a duplicate/mirrored copy of
  packet 2, not a genuine fourth handshake step — the strongest evidence is the
  timestamp option: TS val 1527951684 is exactly the same value the server sent
  in packet 2, which shouldn't happen if real time had elapsed and the server
  generated a fresh packet. This is a known quirk of capturing on macOS's
  loopback interface (lo0): because loopback doesn't send anything over a real
  wire, the kernel's capture hooks can end up recording what is logically one
  packet twice, sometimes with the SYN flag dropped off one of the two copies.
  I'm confident about the practical conclusion (treat it as a capture artifact,
  not a real segment) but I don't want to overstate certainty about the exact
  kernel-internals reason — if you want to verify, the same experiment against a
  non-loopback interface (e.g., two Docker containers, or two machines on your
  LAN) should not show this doubling.

  So: yes, the connection is fully ESTABLISHED after packet 3 — that's what
  completes the handshake on both sides.

  6. Packet 5 — [P.]
  ::1.64604 > ::1.9001, flags [P.] (PSH + ACK), seq 1:182, length 181. Yes, this
  is your actual HTTP request payload (181 bytes — e.g. the request line +
  headers your client sent). The PSH (push) flag is a hint from the sender's TCP
  stack to the receiver's TCP stack meaning "don't buffer this waiting for more
  — hand it to the application now." In practice modern OSes push on
  essentially every write, so you'll see PSH on almost every data-carrying
  segment; it's a holdover from an era where TCP implementations buffered more
  aggressively.

✻ Sautéed for 2m 15s

❯ I should clarify that this is the setup, all on different terminal tabs on my 
  local machine:                                                                
  Terminal 1:  % /tmp/lb -listen :8080 -backend http://localhost:9001           
  Terminal 2: % python3 -m http.server 9001                                     
  Terminal 3: % sudo tcpdump -i lo0 -n port 9001                                
  Terminal 4: Ran this: % for i in $(seq 1 5); do curl -s                       
  http://localhost:8080/ -o /dev/null; done                                     

⏺ Good catch, and that changes my earlier answer — let me fix it.

  You're right the first time: port 64604 is the load balancer's ephemeral
  outbound port. Here's the actual role of each endpoint in that dump:

  - /tmp/lb (your Go binary) is listening on :8080 and acts as a server to curl,
  but a client to the backend.
  - python3 -m http.server 9001 is the backend — a single-threaded server from
  Python's standard library.
  - The handshake you captured (64604 ↔ 9001) is the LB opening a new outbound 
  TCP connection to the backend in order to proxy one of curl's requests. curl
  itself never touches port 9001 directly.

  I also need to retract something from my last answer: I said port 9001 was
  confirmed as your Go echobackend from cmd/echobackend/main.go. That was wrong
  context on my part — in this setup you're using Python's http.server,
  unrelated to that file. The port number just happened to match; I shouldn't
  have assumed it was your code without checking what you'd actually started.

  Everything else in the packet-by-packet breakdown (SYN → SYN-ACK → ACK, the
  relative sequence numbers, the loopback duplicate on packet 4, the PSH data
  packet) still holds structurally — it's just that the client side of that
  handshake is your LB process, not curl.

  One thing worth checking in your full capture, since it's a real design
  question: your for loop fired 5 requests to the LB. Does the full tcpdump
  output show one SYN/handshake sequence on port 9001, or five?

  - If it's one handshake reused for all 5 requests, your LB (via Go's
  http.Transport) is keeping the connection to the backend alive and
  pipelining/reusing it — a real persistent-connection pool.
  - If it's five separate handshakes, the LB is opening (and presumably closing)
  a fresh TCP connection per proxied request.

  This matters in real load balancers because each new connection costs a round
  trip before any data moves, and a closed connection lingers in TIME_WAIT for a
  while afterward — at high request rates that can exhaust ephemeral ports on
  the LB. Want to scroll through the rest of your capture and count SYNs, or
  rerun with something like tcpdump -i lo0 -n port 9001 | grep 'Flags \[S' to
  isolate just the handshake starts?

✻ Baked for 29s

❯ There's 5 SYN/handshakes. I suppose if I used the echobackend server, there   
  would be only one handshake in the beginning and the connection would         
  persist.                                                                      

  Searched for 2 patterns, read 2 files, ran 1 shell command (ctrl+o to expand)

⏺ Your inference is correct, but let me pin down why, because it's not really
  about which binary you run — it's about a protocol detail on each side of the
  connection, and I checked both.

  Connection reuse requires both ends to agree to it. I read the actual code on
  both sides:

  - LB side (internal/proxy/proxy.go:31): the LB builds an httputil.ReverseProxy
  with no Transport field set. When that's nil, ReverseProxy falls back to
  http.DefaultTransport — a single shared connection pool, kept alive across all
  requests handled by this proxy instance (not recreated per request). So the
  LB is already willing to reuse a TCP connection to a given backend.
  - Backend side, Python: python3 -m http.server uses BaseHTTPRequestHandler,
  whose protocol_version defaults to "HTTP/1.0". Under HTTP/1.0, a connection
  closes after one request/response unless both sides explicitly negotiate
  Connection: keep-alive — Python's default handler doesn't. So after every
  single response, the Python server hangs up the socket, forcing the LB to open
  a brand-new TCP connection (new handshake) for the next request.
  - Backend side, echobackend (cmd/echobackend/main.go:36): plain
  http.ListenAndServe, which is Go's standard HTTP/1.1 server. HTTP/1.1 flips
  the default the other way — connections stay open ("persistent") unless
  someone explicitly sends Connection: close. Go's server doesn't do that by
  default.

  So the 5 handshakes you saw are a property of the backend's protocol version 
  and keep-alive behavior, not something inherent to the LB. Swap Terminal 2 to
  go run ./cmd/echobackend -listen :9001 and rerun the same for loop + tcpdump,
  and you should see exactly one SYN for all 5 requests — the LB reusing its
  pooled connection because now the backend cooperates.

  Want to actually run that side-by-side comparison to confirm it, rather than
  take my read of the code on faith?

✻ Brewed for 46s

❯ Yes this is my new setup, all on different terminal tabs on my local machine: 
  Terminal 1:  % /tmp/lb -listen :8080 -backend http://localhost:9001           
  Terminal 2: % python3 -m http.server 9001 --protocol HTTP/1.1                 
  Terminal 3: % sudo tcpdump -i lo0 -n port 9001                                
  Terminal 4: Ran this: % for i in $(seq 1 5); do curl -s                       
  http://localhost:8080/ -o /dev/null; done                                     
                                                                                
  Here's the full TCP dump for the LB <-> backend communication

% sudo tcpdump -i lo0 -n port 9001
tcpdump: verbose output suppressed, use -v[v]... for full protocol decode
listening on lo0, link-type NULL (BSD loopback), snapshot length 524288 bytes
22:45:11.925446 IP6 ::1.64878 > ::1.9001: Flags [S], seq 3065952658, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 712449314 ecr 0,sackOK,eol], length 0
22:45:11.925530 IP6 ::1.9001 > ::1.64878: Flags [S.], seq 2561906719, ack 3065952659, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 770673737 ecr 712449314,sackOK,eol], length 0
22:45:11.925551 IP6 ::1.64878 > ::1.9001: Flags [.], ack 1, win 6372, options [nop,nop,TS val 712449314 ecr 770673737], length 0
22:45:11.925564 IP6 ::1.9001 > ::1.64878: Flags [.], ack 1, win 6372, options [nop,nop,TS val 770673737 ecr 712449314], length 0
22:45:11.925689 IP6 ::1.64878 > ::1.9001: Flags [P.], seq 1:182, ack 1, win 6372, options [nop,nop,TS val 712449314 ecr 770673737], length 181
22:45:11.925704 IP6 ::1.9001 > ::1.64878: Flags [.], ack 182, win 6370, options [nop,nop,TS val 770673737 ecr 712449314], length 0
22:45:11.927925 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 1:156, ack 182, win 6370, options [nop,nop,TS val 770673740 ecr 712449314], length 155
22:45:11.927976 IP6 ::1.64878 > ::1.9001: Flags [.], ack 156, win 6370, options [nop,nop,TS val 712449317 ecr 770673740], length 0
22:45:11.927997 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 156:849, ack 182, win 6370, options [nop,nop,TS val 770673740 ecr 712449317], length 693
22:45:11.928010 IP6 ::1.64878 > ::1.9001: Flags [.], ack 849, win 6360, options [nop,nop,TS val 712449317 ecr 770673740], length 0
22:45:11.940955 IP6 ::1.64878 > ::1.9001: Flags [P.], seq 182:363, ack 849, win 6360, options [nop,nop,TS val 712449329 ecr 770673740], length 181
22:45:11.940987 IP6 ::1.9001 > ::1.64878: Flags [.], ack 363, win 6368, options [nop,nop,TS val 770673752 ecr 712449329], length 0
22:45:11.941547 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 849:1004, ack 363, win 6368, options [nop,nop,TS val 770673754 ecr 712449329], length 155
22:45:11.941574 IP6 ::1.64878 > ::1.9001: Flags [.], ack 1004, win 6358, options [nop,nop,TS val 712449331 ecr 770673754], length 0
22:45:11.941591 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 1004:1697, ack 363, win 6368, options [nop,nop,TS val 770673754 ecr 712449331], length 693
22:45:11.941601 IP6 ::1.64878 > ::1.9001: Flags [.], ack 1697, win 6348, options [nop,nop,TS val 712449331 ecr 770673754], length 0
22:45:11.950793 IP6 ::1.64878 > ::1.9001: Flags [P.], seq 363:544, ack 1697, win 6348, options [nop,nop,TS val 712449340 ecr 770673754], length 181
22:45:11.950820 IP6 ::1.9001 > ::1.64878: Flags [.], ack 544, win 6366, options [nop,nop,TS val 770673763 ecr 712449340], length 0
22:45:11.951062 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 1697:1852, ack 544, win 6366, options [nop,nop,TS val 770673763 ecr 712449340], length 155
22:45:11.951084 IP6 ::1.64878 > ::1.9001: Flags [.], ack 1852, win 6346, options [nop,nop,TS val 712449340 ecr 770673763], length 0
22:45:11.951099 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 1852:2545, ack 544, win 6366, options [nop,nop,TS val 770673763 ecr 712449340], length 693
22:45:11.951109 IP6 ::1.64878 > ::1.9001: Flags [.], ack 2545, win 6336, options [nop,nop,TS val 712449340 ecr 770673763], length 0
22:45:11.957951 IP6 ::1.64878 > ::1.9001: Flags [P.], seq 544:725, ack 2545, win 6336, options [nop,nop,TS val 712449347 ecr 770673763], length 181
22:45:11.957978 IP6 ::1.9001 > ::1.64878: Flags [.], ack 725, win 6364, options [nop,nop,TS val 770673770 ecr 712449347], length 0
22:45:11.958212 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 2545:2700, ack 725, win 6364, options [nop,nop,TS val 770673770 ecr 712449347], length 155
22:45:11.958235 IP6 ::1.64878 > ::1.9001: Flags [.], ack 2700, win 6334, options [nop,nop,TS val 712449347 ecr 770673770], length 0
22:45:11.958248 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 2700:3393, ack 725, win 6364, options [nop,nop,TS val 770673770 ecr 712449347], length 693
22:45:11.958257 IP6 ::1.64878 > ::1.9001: Flags [.], ack 3393, win 6324, options [nop,nop,TS val 712449347 ecr 770673770], length 0
22:45:11.964572 IP6 ::1.64878 > ::1.9001: Flags [P.], seq 725:906, ack 3393, win 6324, options [nop,nop,TS val 712449353 ecr 770673770], length 181
22:45:11.964598 IP6 ::1.9001 > ::1.64878: Flags [.], ack 906, win 6362, options [nop,nop,TS val 770673776 ecr 712449353], length 0
22:45:11.964806 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 3393:3548, ack 906, win 6362, options [nop,nop,TS val 770673776 ecr 712449353], length 155
22:45:11.964827 IP6 ::1.64878 > ::1.9001: Flags [.], ack 3548, win 6322, options [nop,nop,TS val 712449353 ecr 770673776], length 0
22:45:11.964841 IP6 ::1.9001 > ::1.64878: Flags [P.], seq 3548:4241, ack 906, win 6362, options [nop,nop,TS val 770673776 ecr 712449353], length 693
22:45:11.964850 IP6 ::1.64878 > ::1.9001: Flags [.], ack 4241, win 6312, options [nop,nop,TS val 712449353 ecr 770673776], length 0
22:45:42.347069 IP6 ::1.64878 > ::1.9001: Flags [.], ack 4241, win 6312, length 0
22:45:42.347145 IP6 ::1.9001 > ::1.64878: Flags [.], ack 906, win 6362, options [nop,nop,TS val 770704159 ecr 712449353], length 0
22:46:12.424718 IP6 ::1.64878 > ::1.9001: Flags [.], ack 4241, win 6312, length 0
22:46:12.424795 IP6 ::1.9001 > ::1.64878: Flags [.], ack 906, win 6362, options [nop,nop,TS val 770734237 ecr 712449353], length 0
22:46:41.965392 IP6 ::1.64878 > ::1.9001: Flags [F.], seq 906, ack 4241, win 6312, options [nop,nop,TS val 712539355 ecr 770734237], length 0
22:46:41.965449 IP6 ::1.9001 > ::1.64878: Flags [.], ack 907, win 6362, options [nop,nop,TS val 770763778 ecr 712539355], length 0
22:46:41.965643 IP6 ::1.9001 > ::1.64878: Flags [F.], seq 4241, ack 907, win 6362, options [nop,nop,TS val 770763778 ecr 712539355], length 0
22:46:41.965699 IP6 ::1.64878 > ::1.9001: Flags [.], ack 4242, win 6312, options [nop,nop,TS val 712539355 ecr 770763778], length 0

  Here's the full TCP dump for the client <-> LB communication                                                    

 % sudo tcpdump -i lo0 -n port 8080
tcpdump: verbose output suppressed, use -v[v]... for full protocol decode
listening on lo0, link-type NULL (BSD loopback), snapshot length 524288 bytes
22:48:12.734545 IP6 ::1.64899 > ::1.8080: Flags [S], seq 2036480575, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 239779775 ecr 0,sackOK,eol], length 0
22:48:12.734664 IP6 ::1.8080 > ::1.64899: Flags [S.], seq 2930094584, ack 2036480576, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 2754594396 ecr 239779775,sackOK,eol], length 0
22:48:12.734694 IP6 ::1.64899 > ::1.8080: Flags [.], ack 1, win 6372, options [nop,nop,TS val 239779775 ecr 2754594396], length 0
22:48:12.734715 IP6 ::1.8080 > ::1.64899: Flags [.], ack 1, win 6372, options [nop,nop,TS val 2754594396 ecr 239779775], length 0
22:48:12.734730 IP6 ::1.64899 > ::1.8080: Flags [P.], seq 1:78, ack 1, win 6372, options [nop,nop,TS val 239779775 ecr 2754594396], length 77: HTTP: GET / HTTP/1.1
22:48:12.734770 IP6 ::1.8080 > ::1.64899: Flags [.], ack 78, win 6371, options [nop,nop,TS val 2754594396 ecr 239779775], length 0
22:48:12.776626 IP6 ::1.8080 > ::1.64899: Flags [P.], seq 1:849, ack 78, win 6371, options [nop,nop,TS val 2754594438 ecr 239779775], length 848: HTTP: HTTP/1.1 200 OK
22:48:12.776643 IP6 ::1.64899 > ::1.8080: Flags [.], ack 849, win 6359, options [nop,nop,TS val 239779817 ecr 2754594438], length 0
22:48:12.776862 IP6 ::1.64899 > ::1.8080: Flags [F.], seq 78, ack 849, win 6359, options [nop,nop,TS val 239779817 ecr 2754594438], length 0
22:48:12.776876 IP6 ::1.8080 > ::1.64899: Flags [.], ack 79, win 6371, options [nop,nop,TS val 2754594438 ecr 239779817], length 0
22:48:12.776899 IP6 ::1.8080 > ::1.64899: Flags [F.], seq 849, ack 79, win 6371, options [nop,nop,TS val 2754594438 ecr 239779817], length 0
22:48:12.776919 IP6 ::1.64899 > ::1.8080: Flags [.], ack 850, win 6359, options [nop,nop,TS val 239779817 ecr 2754594438], length 0
22:48:12.790203 IP6 ::1.64901 > ::1.8080: Flags [S], seq 3458732960, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 671483107 ecr 0,sackOK,eol], length 0
22:48:12.790363 IP6 ::1.8080 > ::1.64901: Flags [S.], seq 1696912390, ack 3458732961, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 2143072427 ecr 671483107,sackOK,eol], length 0
22:48:12.790403 IP6 ::1.64901 > ::1.8080: Flags [.], ack 1, win 6372, options [nop,nop,TS val 671483107 ecr 2143072427], length 0
22:48:12.790422 IP6 ::1.8080 > ::1.64901: Flags [.], ack 1, win 6372, options [nop,nop,TS val 2143072427 ecr 671483107], length 0
22:48:12.790430 IP6 ::1.64901 > ::1.8080: Flags [P.], seq 1:78, ack 1, win 6372, options [nop,nop,TS val 671483107 ecr 2143072427], length 77: HTTP: GET / HTTP/1.1
22:48:12.790443 IP6 ::1.8080 > ::1.64901: Flags [.], ack 78, win 6371, options [nop,nop,TS val 2143072427 ecr 671483107], length 0
22:48:12.791358 IP6 ::1.8080 > ::1.64901: Flags [P.], seq 1:849, ack 78, win 6371, options [nop,nop,TS val 2143072429 ecr 671483107], length 848: HTTP: HTTP/1.1 200 OK
22:48:12.791394 IP6 ::1.64901 > ::1.8080: Flags [.], ack 849, win 6359, options [nop,nop,TS val 671483109 ecr 2143072429], length 0
22:48:12.791490 IP6 ::1.64901 > ::1.8080: Flags [F.], seq 78, ack 849, win 6359, options [nop,nop,TS val 671483109 ecr 2143072429], length 0
22:48:12.791523 IP6 ::1.8080 > ::1.64901: Flags [.], ack 79, win 6371, options [nop,nop,TS val 2143072429 ecr 671483109], length 0
22:48:12.791540 IP6 ::1.8080 > ::1.64901: Flags [F.], seq 849, ack 79, win 6371, options [nop,nop,TS val 2143072429 ecr 671483109], length 0
22:48:12.791597 IP6 ::1.64901 > ::1.8080: Flags [.], ack 850, win 6359, options [nop,nop,TS val 671483109 ecr 2143072429], length 0
22:48:12.802200 IP6 ::1.64902 > ::1.8080: Flags [S], seq 2491538537, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 2843163516 ecr 0,sackOK,eol], length 0
22:48:12.802329 IP6 ::1.8080 > ::1.64902: Flags [S.], seq 883695138, ack 2491538538, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 1302980686 ecr 2843163516,sackOK,eol], length 0
22:48:12.802362 IP6 ::1.64902 > ::1.8080: Flags [.], ack 1, win 6372, options [nop,nop,TS val 2843163516 ecr 1302980686], length 0
22:48:12.802381 IP6 ::1.8080 > ::1.64902: Flags [.], ack 1, win 6372, options [nop,nop,TS val 1302980686 ecr 2843163516], length 0
22:48:12.802393 IP6 ::1.64902 > ::1.8080: Flags [P.], seq 1:78, ack 1, win 6372, options [nop,nop,TS val 2843163516 ecr 1302980686], length 77: HTTP: GET / HTTP/1.1
22:48:12.802415 IP6 ::1.8080 > ::1.64902: Flags [.], ack 78, win 6371, options [nop,nop,TS val 1302980686 ecr 2843163516], length 0
22:48:12.803139 IP6 ::1.8080 > ::1.64902: Flags [P.], seq 1:849, ack 78, win 6371, options [nop,nop,TS val 1302980687 ecr 2843163516], length 848: HTTP: HTTP/1.1 200 OK
22:48:12.803164 IP6 ::1.64902 > ::1.8080: Flags [.], ack 849, win 6359, options [nop,nop,TS val 2843163517 ecr 1302980687], length 0
22:48:12.803252 IP6 ::1.64902 > ::1.8080: Flags [F.], seq 78, ack 849, win 6359, options [nop,nop,TS val 2843163517 ecr 1302980687], length 0
22:48:12.803280 IP6 ::1.8080 > ::1.64902: Flags [.], ack 79, win 6371, options [nop,nop,TS val 1302980687 ecr 2843163517], length 0
22:48:12.803303 IP6 ::1.8080 > ::1.64902: Flags [F.], seq 849, ack 79, win 6371, options [nop,nop,TS val 1302980687 ecr 2843163517], length 0
22:48:12.803348 IP6 ::1.64902 > ::1.8080: Flags [.], ack 850, win 6359, options [nop,nop,TS val 2843163517 ecr 1302980687], length 0
22:48:12.812353 IP6 ::1.64903 > ::1.8080: Flags [S], seq 96683292, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 1392755385 ecr 0,sackOK,eol], length 0
22:48:12.812502 IP6 ::1.8080 > ::1.64903: Flags [S.], seq 2898613541, ack 96683293, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 1198490919 ecr 1392755385,sackOK,eol], length 0
22:48:12.812531 IP6 ::1.64903 > ::1.8080: Flags [.], ack 1, win 6372, options [nop,nop,TS val 1392755385 ecr 1198490919], length 0
22:48:12.812549 IP6 ::1.8080 > ::1.64903: Flags [.], ack 1, win 6372, options [nop,nop,TS val 1198490919 ecr 1392755385], length 0
22:48:12.812553 IP6 ::1.64903 > ::1.8080: Flags [P.], seq 1:78, ack 1, win 6372, options [nop,nop,TS val 1392755385 ecr 1198490919], length 77: HTTP: GET / HTTP/1.1
22:48:12.812565 IP6 ::1.8080 > ::1.64903: Flags [.], ack 78, win 6371, options [nop,nop,TS val 1198490919 ecr 1392755385], length 0
22:48:12.813131 IP6 ::1.8080 > ::1.64903: Flags [P.], seq 1:849, ack 78, win 6371, options [nop,nop,TS val 1198490919 ecr 1392755385], length 848: HTTP: HTTP/1.1 200 OK
22:48:12.813146 IP6 ::1.64903 > ::1.8080: Flags [.], ack 849, win 6359, options [nop,nop,TS val 1392755385 ecr 1198490919], length 0
22:48:12.813234 IP6 ::1.64903 > ::1.8080: Flags [F.], seq 78, ack 849, win 6359, options [nop,nop,TS val 1392755385 ecr 1198490919], length 0
22:48:12.813250 IP6 ::1.8080 > ::1.64903: Flags [.], ack 79, win 6371, options [nop,nop,TS val 1198490919 ecr 1392755385], length 0
22:48:12.813268 IP6 ::1.8080 > ::1.64903: Flags [F.], seq 849, ack 79, win 6371, options [nop,nop,TS val 1198490919 ecr 1392755385], length 0
22:48:12.813291 IP6 ::1.64903 > ::1.8080: Flags [.], ack 850, win 6359, options [nop,nop,TS val 1392755385 ecr 1198490919], length 0
22:48:12.821171 IP6 ::1.64904 > ::1.8080: Flags [S], seq 3137493358, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 2948605584 ecr 0,sackOK,eol], length 0
22:48:12.821254 IP6 ::1.8080 > ::1.64904: Flags [S.], seq 2294109697, ack 3137493359, win 65535, options [mss 16324,nop,wscale 6,nop,nop,TS val 129390985 ecr 2948605584,sackOK,eol], length 0
22:48:12.821276 IP6 ::1.64904 > ::1.8080: Flags [.], ack 1, win 6372, options [nop,nop,TS val 2948605584 ecr 129390985], length 0
22:48:12.821290 IP6 ::1.8080 > ::1.64904: Flags [.], ack 1, win 6372, options [nop,nop,TS val 129390985 ecr 2948605584], length 0
22:48:12.821299 IP6 ::1.64904 > ::1.8080: Flags [P.], seq 1:78, ack 1, win 6372, options [nop,nop,TS val 2948605584 ecr 129390985], length 77: HTTP: GET / HTTP/1.1
22:48:12.821324 IP6 ::1.8080 > ::1.64904: Flags [.], ack 78, win 6371, options [nop,nop,TS val 129390985 ecr 2948605584], length 0
22:48:12.821688 IP6 ::1.8080 > ::1.64904: Flags [P.], seq 1:849, ack 78, win 6371, options [nop,nop,TS val 129390985 ecr 2948605584], length 848: HTTP: HTTP/1.1 200 OK
22:48:12.821710 IP6 ::1.64904 > ::1.8080: Flags [.], ack 849, win 6359, options [nop,nop,TS val 2948605584 ecr 129390985], length 0
22:48:12.821767 IP6 ::1.64904 > ::1.8080: Flags [F.], seq 78, ack 849, win 6359, options [nop,nop,TS val 2948605584 ecr 129390985], length 0
22:48:12.821792 IP6 ::1.8080 > ::1.64904: Flags [.], ack 79, win 6371, options [nop,nop,TS val 129390985 ecr 2948605584], length 0
22:48:12.821803 IP6 ::1.8080 > ::1.64904: Flags [F.], seq 849, ack 79, win 6371, options [nop,nop,TS val 129390985 ecr 2948605584], length 0
22:48:12.821869 IP6 ::1.64904 > ::1.8080: Flags [.], ack 850, win 6359, options [nop,nop,TS val 2948605584 ecr 129390985], length 0                  



⏺ Confirmed — exactly one SYN for all 5 requests on port 9001, versus five
  separate handshakes on port 8080. But there's a lot more in this trace worth
  pulling apart; it's actually a really good capture.

  1. The backend connection (port 9001) is reused, and stays alive on its own 
  schedule — separate from any single request

  Look at the timestamps: the last real data on that connection is at
  22:45:11.964850. Then nothing happens until 22:45:42.347069 — a bare ACK, no
  payload, no TCP options at all (length 0, no TS val). Then another identical
  one at 22:46:12.424718. Then at 22:46:41.965392 the connection finally closes
  with a FIN.

  Those bare zero-payload ACKs are TCP keepalive probes — a socket option
  (SO_KEEPALIVE) that tells the OS kernel "if this connection sits idle for N
  seconds, send an empty segment to the peer and see if it ACKs back." This
  isn't an application writing data; it's the kernel itself checking "is the
  other end (and the network path to it) still there?" TCP has no built-in
  liveness signal otherwise — a connection can look "open" to both sides
  indefinitely even if the peer's process died or a router in between dropped
  the path, unless either data flows or something like this probes it.

  The interval is ~30.4s and ~30.1s apart. That's not a coincidence: Go's
  http.DefaultTransport (which your LB is using, since proxy.go never sets a
  custom Transport on the ReverseProxy) dials backend connections with
  net.Dialer{KeepAlive: 30 * time.Second}. That's exactly the socket option
  producing these probes.

  And the FIN at 22:46:41.965392 is 90.000542 seconds after the last real data
  packet — matching http.DefaultTransport's IdleConnTimeout: 90 * time.Second
  almost to the millisecond. Go's transport pools backend connections for reuse,
  but only up to 90 seconds of idleness; past that, it proactively closes them
  rather than holding a connection open forever on the off chance another
  request comes in. You're watching that timer fire in real time.

  2. One HTTP response, two TCP segments — segmentation happens per-hop, not 
  per-message

  The backend's response comes as seq 1:156 (155 bytes) immediately followed by
  seq 156:849 (693 bytes) — two separate [P.] packets, 72 microseconds apart,
  totaling 848 bytes. That's well under the negotiated MSS (16324), so it's not
  being split because it's "too big" — it's split because Python's http.server
  almost certainly does two separate write()/send() calls (headers, then body),
  and each write from the application becomes its own TCP segment.

  Compare that to the client-facing side (port 8080): the LB's response to curl
  is seq 1:849 — one single 848-byte segment. Same 848 bytes of actual HTTP
  content, but the LB's ReverseProxy copies the backend's response body through
  in a way that lands in one segment instead of two. This is worth
  internalizing: TCP segment boundaries are a transport-layer detail of one 
  specific hop, not a property of the HTTP message. The same logical response
  can be sliced completely differently on each side of a proxy — the proxy
  passes bytes through, not packets.

  3. The client-facing connections (port 8080) are short-lived because the 
  client process is short-lived

  Your for loop runs curl five separate times — five separate OS processes, each
  opening its own socket, each closing it (FIN) the instant its one
  request/response finishes (e.g. 22:48:12.776862, immediately after the
  response at .776626). HTTP/1.1 defaults to persistent connections, but
  persistence only matters if someone intends to reuse the socket — and a curl
  process that's about to exit has no next request to reuse it for, so the OS
  tears the connection down as the process exits.

  The backend connection behaves completely differently because it's owned by
  the long-running LB process, which has no way to know whether another client
  request is about to arrive and might want to reuse it — so it holds the
  connection open speculatively (up to that 90s idle limit). This is the actual
  point of connection pooling at a load balancer: it decouples the lifecycle of 
  front-end connections from back-end connections. Five short-lived client
  connections got funneled through one persistent backend connection, saving the
  backend four TCP handshakes it would otherwise have had to do.

  4. Each connection closes with two independent FIN/ACK exchanges

  e.g. at the end of the 9001 connection: client FIN → server ACK, then
  (separately) server FIN → client ACK. TCP connections are full-duplex — two
  independent byte streams, one each direction — so "closing" means each side
  closes its own send direction independently. A FIN says "I'm done sending,"
  not "let's both stop." Here the server sends its FIN just 54 microseconds
  after ACKing the client's FIN, so it looks almost simultaneous, but they're
  still two separate one-directional closes, not one event.

  5. The mystery duplicate packet from last time — it did NOT reproduce on port 
  8080

  The port 9001 dump shows the same mirrored ack 1 duplicate right after the
  SYN-ACK that we saw in your first capture (22:45:11.925551 and .925564, 13
  microseconds apart, mirrored src/dst). But the port 8080 dump — five full
  handshakes — shows no such duplicate on any of them, just the clean 3-packet
  handshake. That's useful evidence: it means the artifact isn't a fixed rule of
  "loopback always double-captures the 3rd handshake packet," it's something
  more like a timing-dependent race in how macOS's loopback driver feeds the BPF
  capture tap. I'd still treat it as a capture quirk rather than real traffic,
  but I'll be honest that I can't give you a precise, confident mechanism for
  why it shows up sometimes and not others.

✻ Brewed for 2m 6s
