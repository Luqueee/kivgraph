package tsworker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/procstat"
)

// State is the observable lifecycle state of a supervisor.
type State string

const (
	// StateStopped is the state before Start and after a clean Close.
	StateStopped State = "STOPPED"
	// StateStarting covers spawning the process and the handshake.
	StateStarting State = "STARTING"
	// StateReady means a handshaked session is accepting requests.
	StateReady State = "READY"
	// StateRestarting means the session was invalidated and is being replaced.
	StateRestarting State = "RESTARTING"
	// StateFailed means the restart budget is exhausted; requests fail fast.
	StateFailed State = "FAILED"
	// StateClosed is terminal.
	StateClosed State = "CLOSED"
)

// Supervisor-local error codes. They are a separate namespace from the
// protocol codes in messages.go, which are produced by the worker.
const (
	CodeRestartLimit   = "RESTART_LIMIT"
	CodeClosed         = "SUPERVISOR_CLOSED"
	CodeTimeout        = "TIMEOUT"
	CodeHandshake      = "HANDSHAKE_FAILED"
	CodeSpawn          = "SPAWN_FAILED"
	CodeWorkerExited   = "WORKER_EXITED"
	CodeShutdownForced = "SHUTDOWN_FORCED"
	CodeNotStarted     = "NOT_STARTED"
)

// Protocol limits from section 7 of docs/protocol/ts-worker-v1.md.
const (
	defaultHandshakeTimeout = 5 * time.Second
	defaultShutdownGrace    = 5 * time.Second
	defaultRequestTimeout   = 30 * time.Second
	defaultRestartLimit     = 3
	defaultRestartWindow    = time.Minute
	defaultRestartBackoff   = 200 * time.Millisecond
	defaultStderrTailLines  = 64
)

// SupervisorError is a classified supervisor failure. Callers branch on Code,
// never on the message text.
type SupervisorError struct {
	Code string
	Op   string
	Err  error
}

func (err *SupervisorError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Err == nil {
		return fmt.Sprintf("tsworker %s: %s", err.Op, err.Code)
	}
	return fmt.Sprintf("tsworker %s: %s: %v", err.Op, err.Code, err.Err)
}

func (err *SupervisorError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func newSupervisorError(code, op string, cause error) error {
	return &SupervisorError{Code: code, Op: op, Err: cause}
}

// ErrorCode returns the classified code of err, or an empty string when err
// carries none. It sees supervisor failures, worker replies and framing.
func ErrorCode(err error) string {
	var supervisorErr *SupervisorError
	if errors.As(err, &supervisorErr) {
		return supervisorErr.Code
	}
	var workerErr *WorkerError
	if errors.As(err, &workerErr) {
		return workerErr.Code
	}
	var framing *FramingError
	if errors.As(err, &framing) {
		return string(framing.Kind)
	}
	return ""
}

// Event is a worker-initiated frame. Events carry id zero and are delivered in
// the session read loop, so a slow handler applies backpressure to the worker.
type Event struct {
	Generation uint64
	Type       string
	Payload    json.RawMessage
}

// SessionLoss reports an invalidated session. Pending lists the requests that
// were aborted; any batch they had started emitting is incomplete and must be
// discarded entirely, per section 6 of the protocol.
type SessionLoss struct {
	Generation  uint64
	Pending     []uint64
	Err         error
	WillRestart bool
}

// Options configures a Supervisor. Zero values fall back to the protocol
// defaults.
type Options struct {
	// Command and Args launch the worker.
	Command string
	Args    []string
	// Dir and Env are passed to the process; a nil Env inherits the parent.
	Dir string
	Env []string
	// SupervisorVersion is announced in HELLO.
	SupervisorVersion string
	// ProtocolVersions is the offer sent in HELLO. Defaults to ProtocolVersion.
	ProtocolVersions []int

	HandshakeTimeout time.Duration
	RequestTimeout   time.Duration
	ShutdownGrace    time.Duration

	// RestartLimit is the number of restarts tolerated inside RestartWindow
	// before the supervisor gives up. A crash loop must stop, not spin.
	RestartLimit   int
	RestartWindow  time.Duration
	RestartBackoff time.Duration

	// StderrTailLines bounds the retained stderr tail.
	StderrTailLines int

	// Metrics receives worker restart and best-effort resident-memory
	// observations when configured. Non-Linux platforms report zero memory.
	Metrics *metrics.Registry

	// OnStderr receives every stderr line. stderr never carries protocol data.
	OnStderr func(line string)
	// OnEvent receives worker-initiated frames.
	OnEvent func(Event)
	// OnSessionLost receives every session invalidation.
	OnSessionLost func(SessionLoss)
}

func (options *Options) applyDefaults() {
	if options.HandshakeTimeout <= 0 {
		options.HandshakeTimeout = defaultHandshakeTimeout
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaultRequestTimeout
	}
	if options.ShutdownGrace <= 0 {
		options.ShutdownGrace = defaultShutdownGrace
	}
	if options.RestartLimit < 0 {
		options.RestartLimit = 0
	} else if options.RestartLimit == 0 {
		options.RestartLimit = defaultRestartLimit
	}
	if options.RestartWindow <= 0 {
		options.RestartWindow = defaultRestartWindow
	}
	if options.RestartBackoff < 0 {
		options.RestartBackoff = 0
	}
	if options.StderrTailLines <= 0 {
		options.StderrTailLines = defaultStderrTailLines
	}
	if len(options.ProtocolVersions) == 0 {
		options.ProtocolVersions = []int{ProtocolVersion}
	}
}

// Status is a snapshot of the observable supervisor state. StderrTail carries
// worker output only; supervisor anomalies are counted, never mixed into it.
type Status struct {
	State            State
	Generation       uint64
	PID              int
	Starts           int
	Restarts         int
	ForcedKills      int
	PendingRequests  int
	UnmatchedReplies int
	InvalidFrames    int
	Uptime           time.Duration
	Handshake        *HelloResponse
	LastError        string
	StderrTail       []string
}

type call struct {
	id          uint64
	generation  uint64
	messageType string
	done        chan callResult
}

type callResult struct {
	messageType string
	payload     json.RawMessage
	err         error
}

// session owns one worker process. Exactly one goroutine reaps it, because
// os/exec closes the pipes during Wait and a concurrent read would race.
type session struct {
	generation uint64
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	reader     *Reader
	writer     *Writer
	info       HelloResponse
	started    time.Time

	expectExit bool

	readOnce sync.Once
	readDone chan struct{}
	drained  chan struct{}
	exited   chan struct{}
	waitErr  error
}

// endRead reports that nobody will read the transport again. The reaper waits
// for this before calling Wait.
func (sess *session) endRead() {
	sess.readOnce.Do(func() { close(sess.readDone) })
}

// reap is the single owner of cmd.Wait.
func (sess *session) reap() {
	<-sess.readDone
	<-sess.drained
	sess.reader.Close()
	sess.writer.Close()
	_ = sess.stdin.Close()
	if err := sess.cmd.Wait(); err != nil {
		sess.waitErr = newSupervisorError(CodeWorkerExited, "wait", err)
	}
	close(sess.exited)
}

// waitExit reports whether the process was reaped within the grace period.
func (sess *session) waitExit(grace time.Duration) bool {
	if grace <= 0 {
		grace = time.Second
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-sess.exited:
		return true
	case <-timer.C:
		return false
	}
}

func (sess *session) pid() int {
	if sess == nil || sess.cmd == nil || sess.cmd.Process == nil {
		return 0
	}
	return sess.cmd.Process.Pid
}

// Supervisor owns the worker process lifecycle: spawn, handshake, request
// correlation, cancellation, restart with a bounded budget and shutdown.
type Supervisor struct {
	options Options

	mu          sync.Mutex
	state       State
	closed      bool
	current     *session
	readyCh     chan struct{}
	pending     map[uint64]*call
	nextID      uint64
	generation  uint64
	starts      int
	restarts    int
	forcedKills int
	unmatched   int
	invalid     int
	shutdownID  uint64
	restartLog  []time.Time
	lastErr     error
	stderr      *lineRing
	watchDone   chan struct{}
}

// NewSupervisor builds a stopped supervisor.
func NewSupervisor(options Options) (*Supervisor, error) {
	if options.Command == "" {
		return nil, newSupervisorError(CodeSpawn, "configure", errors.New("command must not be empty"))
	}
	options.applyDefaults()
	return &Supervisor{
		options: options,
		state:   StateStopped,
		readyCh: make(chan struct{}),
		pending: make(map[uint64]*call),
		stderr:  newLineRing(options.StderrTailLines),
	}, nil
}

// Start spawns the worker and completes the handshake. It returns only once
// the session is ready, so a caller that gets nil can issue requests.
func (supervisor *Supervisor) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	supervisor.mu.Lock()
	switch {
	case supervisor.closed:
		supervisor.mu.Unlock()
		return newSupervisorError(CodeClosed, "start", nil)
	case supervisor.state == StateReady || supervisor.state == StateStarting || supervisor.state == StateRestarting:
		supervisor.mu.Unlock()
		return newSupervisorError(CodeSpawn, "start", errors.New("supervisor is already running"))
	}
	supervisor.state = StateStarting
	supervisor.mu.Unlock()

	sess, err := supervisor.startSession(ctx)
	if err != nil {
		supervisor.mu.Lock()
		supervisor.state = StateStopped
		supervisor.lastErr = err
		supervisor.mu.Unlock()
		return err
	}

	watchDone := make(chan struct{})
	supervisor.mu.Lock()
	supervisor.current = sess
	supervisor.starts++
	supervisor.watchDone = watchDone
	supervisor.markReadyLocked()
	supervisor.mu.Unlock()

	go supervisor.watch(sess, watchDone)
	return nil
}

// markReadyLocked publishes readiness to every waiter.
func (supervisor *Supervisor) markReadyLocked() {
	if supervisor.state != StateReady {
		close(supervisor.readyCh)
	}
	supervisor.state = StateReady
}

// markNotReadyLocked reopens the readiness gate for the next session.
func (supervisor *Supervisor) markNotReadyLocked(state State) {
	if supervisor.state == StateReady {
		supervisor.readyCh = make(chan struct{})
	}
	supervisor.state = state
}

// Status returns a snapshot of the supervisor state.
func (supervisor *Supervisor) Status() Status {
	supervisor.mu.Lock()

	status := Status{
		State:            supervisor.state,
		Generation:       supervisor.generation,
		Starts:           supervisor.starts,
		Restarts:         supervisor.restarts,
		ForcedKills:      supervisor.forcedKills,
		PendingRequests:  len(supervisor.pending),
		UnmatchedReplies: supervisor.unmatched,
		InvalidFrames:    supervisor.invalid,
		StderrTail:       supervisor.stderr.lines(),
	}
	if supervisor.lastErr != nil {
		status.LastError = supervisor.lastErr.Error()
	}
	if sess := supervisor.current; sess != nil {
		info := sess.info
		status.Handshake = &info
		status.Uptime = time.Since(sess.started)
		status.PID = sess.pid()
	}
	supervisor.mu.Unlock()
	if supervisor.options.Metrics != nil {
		supervisor.options.Metrics.ObserveWorker(metrics.WorkerObservation{
			Restarts:    uint64(status.Restarts),
			MemoryBytes: procstat.ResidentBytes(status.PID),
		})
	}
	return status
}

// Call sends a request and waits for the reply with the same id.
//
// The two failure modes are deliberately different. A caller cancelling ctx
// sends CANCEL and keeps the session, per section 3.7. A request timeout
// invalidates the session, because section 6 classifies a timeout among the
// conditions after which the worker state can no longer be trusted.
func (supervisor *Supervisor) Call(ctx context.Context, messageType string, payload any) (json.RawMessage, error) {
	if messageType == "" {
		return nil, newSupervisorError(CodeInvalidPayload, "call", errors.New("message type must not be empty"))
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sess, err := supervisor.waitReady(ctx)
	if err != nil {
		return nil, err
	}

	pendingCall := supervisor.register(sess, messageType)
	defer supervisor.unregister(pendingCall.id)

	envelope, err := NewEnvelope(pendingCall.id, messageType, payload)
	if err != nil {
		return nil, err
	}
	if err := sess.writer.WriteFrame(envelope); err != nil {
		supervisor.invalidate(sess, err)
		return nil, err
	}

	timer := time.NewTimer(supervisor.options.RequestTimeout)
	defer timer.Stop()

	select {
	case result := <-pendingCall.done:
		if result.err != nil {
			return nil, result.err
		}
		if result.messageType == MessageError {
			return nil, decodeWorkerError(messageType, result.payload)
		}
		return result.payload, nil
	case <-ctx.Done():
		supervisor.requestCancel(sess, pendingCall.id)
		return nil, newSupervisorError(CodeCanceled, "call", ctx.Err())
	case <-timer.C:
		timeoutErr := newSupervisorError(CodeTimeout, "call", fmt.Errorf("%s exceeded %s", messageType, supervisor.options.RequestTimeout))
		supervisor.invalidate(sess, timeoutErr)
		return nil, timeoutErr
	}
}

// Cancel asks the worker to abandon a request already in flight. Cancelling an
// unknown or finished id is not an error, per section 3.7.
func (supervisor *Supervisor) Cancel(ctx context.Context, targetID uint64) error {
	sess, err := supervisor.waitReady(ctx)
	if err != nil {
		return err
	}
	return supervisor.sendCancel(sess, targetID)
}

func (supervisor *Supervisor) requestCancel(sess *session, targetID uint64) {
	// Best effort: the caller already gave up, and a failed CANCEL surfaces
	// through the session invalidation path instead.
	_ = supervisor.sendCancel(sess, targetID)
}

func (supervisor *Supervisor) sendCancel(sess *session, targetID uint64) error {
	envelope, err := NewEnvelope(supervisor.takeID(), MessageCancel, CancelRequest{TargetID: targetID})
	if err != nil {
		return err
	}
	if err := sess.writer.WriteFrame(envelope); err != nil {
		supervisor.invalidate(sess, err)
		return err
	}
	return nil
}

func (supervisor *Supervisor) takeID() uint64 {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.nextID++
	return supervisor.nextID
}

// waitReady blocks until a handshaked session exists or the wait is hopeless.
func (supervisor *Supervisor) waitReady(ctx context.Context) (*session, error) {
	for {
		supervisor.mu.Lock()
		switch {
		case supervisor.closed || supervisor.state == StateClosed:
			supervisor.mu.Unlock()
			return nil, newSupervisorError(CodeClosed, "wait ready", nil)
		case supervisor.state == StateFailed:
			cause := supervisor.lastErr
			supervisor.mu.Unlock()
			return nil, newSupervisorError(CodeRestartLimit, "wait ready", cause)
		case supervisor.state == StateStopped:
			supervisor.mu.Unlock()
			return nil, newSupervisorError(CodeNotStarted, "wait ready", errors.New("supervisor is not started"))
		case supervisor.state == StateReady && supervisor.current != nil:
			sess := supervisor.current
			supervisor.mu.Unlock()
			return sess, nil
		}
		ready := supervisor.readyCh
		supervisor.mu.Unlock()

		select {
		case <-ready:
		case <-ctx.Done():
			return nil, newSupervisorError(CodeCanceled, "wait ready", ctx.Err())
		}
	}
}

func (supervisor *Supervisor) register(sess *session, messageType string) *call {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()

	supervisor.nextID++
	pendingCall := &call{
		id:          supervisor.nextID,
		generation:  sess.generation,
		messageType: messageType,
		done:        make(chan callResult, 1),
	}
	supervisor.pending[pendingCall.id] = pendingCall
	return pendingCall
}

func (supervisor *Supervisor) unregister(id uint64) {
	supervisor.mu.Lock()
	delete(supervisor.pending, id)
	supervisor.mu.Unlock()
}

// dispatch routes one decoded frame. The payload is already copied out of the
// reader buffer, which the next read reuses.
func (supervisor *Supervisor) dispatch(sess *session, envelope Envelope, payload json.RawMessage) {
	if envelope.ID == 0 {
		if handler := supervisor.options.OnEvent; handler != nil {
			handler(Event{Generation: sess.generation, Type: envelope.Type, Payload: payload})
		}
		return
	}

	supervisor.mu.Lock()
	pendingCall, ok := supervisor.pending[envelope.ID]
	if ok && pendingCall.generation == sess.generation {
		delete(supervisor.pending, envelope.ID)
	} else {
		ok = false
		// The SHUTDOWN reply is expected to have no waiter: Close watches the
		// process exit, which is the stronger signal.
		if envelope.ID != supervisor.shutdownID {
			supervisor.unmatched++
		}
	}
	supervisor.mu.Unlock()

	if !ok {
		// A reply nobody waits for is a worker protocol violation, but it
		// cannot corrupt the stream: count it and keep reading.
		return
	}
	pendingCall.done <- callResult{messageType: envelope.Type, payload: payload}
}

// watch reads frames until the session ends, then restarts within budget.
func (supervisor *Supervisor) watch(sess *session, done chan struct{}) {
	defer close(done)
	for {
		cause := supervisor.readLoop(sess)
		next, keepGoing := supervisor.afterLoss(sess, cause)
		if !keepGoing {
			return
		}
		sess = next
	}
}

// readLoop returns the classified cause that ended the session.
func (supervisor *Supervisor) readLoop(sess *session) error {
	defer sess.endRead()
	for {
		envelope, err := sess.reader.ReadFrame(context.Background())
		if err != nil {
			var framing *FramingError
			if errors.As(err, &framing) && !framing.Fatal() {
				// An invalid body keeps the stream aligned: the frame boundary
				// was honoured, so the session survives.
				supervisor.countInvalidFrame()
				continue
			}
			return err
		}
		payload := append(json.RawMessage(nil), envelope.Payload...)
		supervisor.dispatch(sess, envelope, payload)
	}
}

// afterLoss fails every pending request of the dead session, reports the loss
// and restarts while the budget allows it.
func (supervisor *Supervisor) afterLoss(sess *session, cause error) (*session, bool) {
	sess.waitExit(supervisor.options.ShutdownGrace)
	if cause == nil {
		cause = sess.waitErr
	}

	supervisor.mu.Lock()
	expected := sess.expectExit
	closed := supervisor.closed
	if supervisor.current == sess {
		supervisor.current = nil
	}
	aborted := supervisor.failPendingLocked(sess.generation, cause)
	willRestart := !expected && !closed && supervisor.restartAllowedLocked()
	if !expected && !closed {
		supervisor.lastErr = cause
	}
	if !closed {
		switch {
		case willRestart:
			supervisor.markNotReadyLocked(StateRestarting)
		case expected:
			supervisor.markNotReadyLocked(StateStopped)
		default:
			supervisor.markNotReadyLocked(StateFailed)
		}
	}
	supervisor.mu.Unlock()

	if handler := supervisor.options.OnSessionLost; handler != nil && !expected {
		handler(SessionLoss{
			Generation:  sess.generation,
			Pending:     aborted,
			Err:         cause,
			WillRestart: willRestart,
		})
	}
	if !willRestart {
		return nil, false
	}
	return supervisor.restart()
}

// restart retries the spawn and handshake until the budget runs out.
func (supervisor *Supervisor) restart() (*session, bool) {
	for {
		supervisor.mu.Lock()
		if supervisor.closed || !supervisor.restartAllowedLocked() {
			if !supervisor.closed {
				supervisor.markNotReadyLocked(StateFailed)
			}
			supervisor.mu.Unlock()
			return nil, false
		}
		supervisor.recordRestartLocked()
		backoff := supervisor.options.RestartBackoff
		supervisor.mu.Unlock()

		if backoff > 0 {
			time.Sleep(backoff)
		}

		sess, err := supervisor.startSession(context.Background())
		if err != nil {
			supervisor.mu.Lock()
			supervisor.lastErr = err
			supervisor.mu.Unlock()
			continue
		}

		supervisor.mu.Lock()
		if supervisor.closed {
			supervisor.mu.Unlock()
			supervisor.discard(sess)
			return nil, false
		}
		supervisor.current = sess
		supervisor.starts++
		supervisor.markReadyLocked()
		supervisor.mu.Unlock()
		return sess, true
	}
}

// discard terminates a session nobody will use.
func (supervisor *Supervisor) discard(sess *session) {
	sess.expectExit = true
	_ = killTree(sess.cmd)
	sess.endRead()
	sess.waitExit(supervisor.options.ShutdownGrace)
}

// restartAllowedLocked reports whether another restart fits in the window.
func (supervisor *Supervisor) restartAllowedLocked() bool {
	supervisor.pruneRestartsLocked()
	return len(supervisor.restartLog) < supervisor.options.RestartLimit
}

func (supervisor *Supervisor) recordRestartLocked() {
	supervisor.pruneRestartsLocked()
	supervisor.restartLog = append(supervisor.restartLog, time.Now())
	supervisor.restarts++
	supervisor.options.Metrics.RecordWorkerRestart()
}

func (supervisor *Supervisor) pruneRestartsLocked() {
	cutoff := time.Now().Add(-supervisor.options.RestartWindow)
	kept := supervisor.restartLog[:0]
	for _, moment := range supervisor.restartLog {
		if moment.After(cutoff) {
			kept = append(kept, moment)
		}
	}
	supervisor.restartLog = kept
}

// failPendingLocked aborts the requests of one generation and returns their
// ids so the caller can discard whatever partial batch they had produced.
func (supervisor *Supervisor) failPendingLocked(generation uint64, cause error) []uint64 {
	var aborted []uint64
	for id, pendingCall := range supervisor.pending {
		if pendingCall.generation != generation {
			continue
		}
		delete(supervisor.pending, id)
		aborted = append(aborted, id)
		pendingCall.done <- callResult{err: newSupervisorError(CodeEngineUnavailable, "call", cause)}
	}
	return aborted
}

// invalidate ends a session the caller found unusable. The watch goroutine
// observes the read failure and performs the restart.
func (supervisor *Supervisor) invalidate(sess *session, cause error) {
	supervisor.mu.Lock()
	if supervisor.current == sess {
		supervisor.lastErr = cause
	}
	supervisor.mu.Unlock()
	_ = killTree(sess.cmd)
}

// Close shuts the worker down: SHUTDOWN, then the grace period, then the
// process group. It returns CodeShutdownForced when the worker had to be
// terminated, so a degraded shutdown is visible instead of silent.
func (supervisor *Supervisor) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.closed = true
	sess := supervisor.current
	watchDone := supervisor.watchDone
	if sess != nil {
		sess.expectExit = true
		supervisor.failPendingLocked(sess.generation, newSupervisorError(CodeClosed, "close", nil))
	}
	supervisor.markNotReadyLocked(StateClosed)
	supervisor.mu.Unlock()

	var closeErr error
	if sess != nil {
		closeErr = supervisor.shutdownSession(ctx, sess)
	}
	if watchDone != nil {
		<-watchDone
	}

	supervisor.mu.Lock()
	supervisor.current = nil
	supervisor.state = StateClosed
	supervisor.mu.Unlock()
	return closeErr
}

// shutdownSession asks politely, waits the grace period, then escalates.
func (supervisor *Supervisor) shutdownSession(ctx context.Context, sess *session) error {
	shutdownID := supervisor.takeID()
	supervisor.mu.Lock()
	supervisor.shutdownID = shutdownID
	supervisor.mu.Unlock()

	if envelope, err := NewEnvelope(shutdownID, MessageShutdown, struct{}{}); err == nil {
		_ = sess.writer.WriteFrame(envelope)
	}
	// Closing stdin is the second signal: a worker blocked on read sees EOF.
	_ = sess.stdin.Close()

	grace := supervisor.options.ShutdownGrace
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < grace {
			grace = remaining
		}
	}
	if sess.waitExit(grace) {
		return nil
	}

	supervisor.mu.Lock()
	supervisor.forcedKills++
	supervisor.mu.Unlock()

	_ = interruptTree(sess.cmd)
	if sess.waitExit(grace) {
		return newSupervisorError(CodeShutdownForced, "close", errors.New("worker ignored SHUTDOWN and was terminated"))
	}
	_ = killTree(sess.cmd)
	sess.waitExit(grace)
	return newSupervisorError(CodeShutdownForced, "close", errors.New("worker ignored SHUTDOWN and was killed"))
}

// startSession spawns the process and completes the handshake.
func (supervisor *Supervisor) startSession(ctx context.Context) (*session, error) {
	cmd := exec.Command(supervisor.options.Command, supervisor.options.Args...)
	cmd.Dir = supervisor.options.Dir
	cmd.Env = supervisor.options.Env
	isolateProcess(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, newSupervisorError(CodeSpawn, "spawn", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, newSupervisorError(CodeSpawn, "spawn", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, newSupervisorError(CodeSpawn, "spawn", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, newSupervisorError(CodeSpawn, "spawn", err)
	}

	supervisor.mu.Lock()
	supervisor.generation++
	generation := supervisor.generation
	supervisor.mu.Unlock()

	sess := &session{
		generation: generation,
		cmd:        cmd,
		stdin:      stdin,
		reader:     NewReader(stdout),
		writer:     NewWriter(stdin),
		started:    time.Now(),
		readDone:   make(chan struct{}),
		drained:    make(chan struct{}),
		exited:     make(chan struct{}),
	}
	go supervisor.pumpStderr(stderr, sess.drained)
	go sess.reap()

	handshakeCtx, cancel := context.WithTimeout(ctx, supervisor.options.HandshakeTimeout)
	defer cancel()

	info, err := supervisor.handshake(handshakeCtx, sess)
	if err != nil {
		supervisor.discard(sess)
		return nil, err
	}
	sess.info = info
	return sess, nil
}

// handshake performs HELLO. Any other frame before the reply is a protocol
// violation: the contract says HELLO is the first message of the session.
func (supervisor *Supervisor) handshake(ctx context.Context, sess *session) (HelloResponse, error) {
	id := supervisor.takeID()
	request := HelloRequest{
		ProtocolVersions:  supervisor.options.ProtocolVersions,
		SupervisorVersion: supervisor.options.SupervisorVersion,
	}
	envelope, err := NewEnvelope(id, MessageHello, request)
	if err != nil {
		return HelloResponse{}, err
	}
	if err := sess.writer.WriteFrame(envelope); err != nil {
		return HelloResponse{}, newSupervisorError(CodeHandshake, "handshake", err)
	}

	reply, err := sess.reader.ReadFrame(ctx)
	if err != nil {
		code := CodeHandshake
		var framing *FramingError
		if errors.As(err, &framing) && (framing.Kind == Timeout || framing.Kind == Canceled) {
			code = CodeTimeout
		}
		return HelloResponse{}, newSupervisorError(code, "handshake", err)
	}
	if reply.ID != id {
		return HelloResponse{}, newSupervisorError(CodeHandshake, "handshake", fmt.Errorf("expected reply id %d, got %d", id, reply.ID))
	}
	if reply.Type == MessageError {
		return HelloResponse{}, decodeWorkerError(MessageHello, append(json.RawMessage(nil), reply.Payload...))
	}
	if reply.Type != MessageHello {
		return HelloResponse{}, newSupervisorError(CodeHandshake, "handshake", fmt.Errorf("expected %s reply, got %s", MessageHello, reply.Type))
	}

	var info HelloResponse
	if err := json.Unmarshal(reply.Payload, &info); err != nil {
		return HelloResponse{}, newSupervisorError(CodeInvalidPayload, "handshake", err)
	}
	if err := info.Validate(supervisor.options.ProtocolVersions); err != nil {
		return HelloResponse{}, newSupervisorError(CodeVersionMismatch, "handshake", err)
	}
	return info, nil
}

// pumpStderr keeps operational logs out of the protocol stream.
func (supervisor *Supervisor) pumpStderr(stderr io.Reader, drained chan struct{}) {
	defer close(drained)
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 8192), 1<<20)
	for scanner.Scan() {
		supervisor.recordStderr(scanner.Text())
	}
}

// recordStderr keeps one worker output line. It never carries protocol data.
func (supervisor *Supervisor) recordStderr(line string) {
	supervisor.mu.Lock()
	supervisor.stderr.add(line)
	supervisor.mu.Unlock()
	if handler := supervisor.options.OnStderr; handler != nil {
		handler(line)
	}
}

// countInvalidFrame records a recoverable decode failure. The session stays
// aligned, so this is an anomaly to observe, not a reason to restart.
func (supervisor *Supervisor) countInvalidFrame() {
	supervisor.mu.Lock()
	supervisor.invalid++
	supervisor.mu.Unlock()
}

func decodeWorkerError(request string, payload json.RawMessage) error {
	var body ErrorPayload
	if err := json.Unmarshal(payload, &body); err != nil {
		return newSupervisorError(CodeInvalidPayload, "decode error", err)
	}
	if body.Code == "" {
		return newSupervisorError(CodeInvalidPayload, "decode error", errors.New("error payload without code"))
	}
	return &WorkerError{Request: request, Code: body.Code, Message: body.Message, Retryable: body.Retryable}
}

// lineRing keeps a bounded tail of stderr lines.
type lineRing struct {
	entries []string
	next    int
	full    bool
}

func newLineRing(capacity int) *lineRing {
	return &lineRing{entries: make([]string, capacity)}
}

func (ring *lineRing) add(line string) {
	ring.entries[ring.next] = line
	ring.next = (ring.next + 1) % len(ring.entries)
	if ring.next == 0 {
		ring.full = true
	}
}

func (ring *lineRing) lines() []string {
	if !ring.full {
		return append([]string(nil), ring.entries[:ring.next]...)
	}
	out := make([]string, 0, len(ring.entries))
	out = append(out, ring.entries[ring.next:]...)
	return append(out, ring.entries[:ring.next]...)
}
