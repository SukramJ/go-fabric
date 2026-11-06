// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/camera"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The camera acceptance of phase 37 (issue 410, brief
// notes/briefs/phase-37-camera-controller.md §5): chip's camera-app in the
// harness, one WebRTC session end to end, the SDP over TCP. The Go
// controller with the camera service (controller/camera) is the DUT — the
// WebRtcTransportRequestor and the stream manager — and chip-camera-app,
// built at 6170af84, the TH_SERVER, as matter.js's TC-WEBRTCR cases place
// them (support/chip-testing/test/cert/tc-webrtcr-support.ts:142-148 at
// 862ad75f). It is no matter.js case file: matter.js's WEBRTCR files drive
// chip's prompt-driven Python scripts, which stay the harness's follow-up
// (families_table_test.go requestorServed). The family key CAMERA-GO names
// this module's own acceptance; it is no chip case prefix, so
// TestUnfamiliedPrefixes does not count it, and the status page lists it
// under the cases run with the Go controller as DUT, in no family.
//
// chip-camera-app runs in a container of its own from the harness image,
// with host networking and the host's D-Bus as the harness container has
// them (harness_test.go launchHarness): its own KVS inside the container,
// a free secured port, the leg's discriminator and passcode, and
// --camera-test-videosrc, the GStreamer test pattern in place of a video
// device (examples/platform/linux/Options.cpp:498-499 at 6170af84). The
// app serves Matter over TCP: chip_inet_config_enable_tcp_endpoint
// defaults to true (src/inet/inet.gni:28 at 6170af84), and the camera's
// args.gni turns IPv4 off "to force IPv6 usage for TCP connections"
// (examples/camera-app/linux/args.gni:33-34 at 6170af84); step 1 holds the
// app's "TCP server listening" line to that. --camera-deferred-offer is
// not passed: it is the deferred-offer hint of a SolicitOffer
// (webrtc-provider-manager.cpp:220 at 6170af84), and the session here
// starts with ProvideOffer.

func init() { registerGoCert(caseCameraGo) }

// cameraGoOrigin is the case's golden source.
const cameraGoOrigin = "the camera acceptance of issue 410: one WebRTC session against chip-camera-app at 6170af84, the SDP over TCP (gocert_camera_test.go)"

// The camera-app's commissioning identity, the leg's own.
const (
	cameraGoDiscriminator = 3737
	cameraGoPasscode      = 20203737
)

// cameraGoFingerprint is a DTLS fingerprint of the sha-256 form, which the
// camera-app requires of an offer but never checks against a handshake —
// the session ends before any DTLS.
var cameraGoFingerprint = "sha-256 " + strings.TrimSuffix(strings.Repeat("5A:", 32), ":")

// cameraGoOffer is the offer of the start: matterjs-server's H.264
// video-only offer (packages/ws-controller/test/WebSocketRequestLoggingTest.ts:23-36
// at 0bd06f2) with the fields chip-camera-app requires of every SDP —
// a=fingerprint beside the ICE credentials (ValidateSdpFields,
// examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:66-104
// at 6170af84) — and the a=mid, a=setup and a=rtcp-mux lines a WebRTC
// offer carries for libdatachannel to answer it (:500-513 takes the video section's mid).
var cameraGoOffer = strings.Join([]string{
	"v=0",
	"o=- 1 1 IN IP4 127.0.0.1",
	"s=-",
	"t=0 0",
	"m=video 9 UDP/TLS/RTP/SAVPF 96",
	"c=IN IP4 0.0.0.0",
	"a=mid:0",
	"a=recvonly",
	"a=rtcp-mux",
	"a=setup:actpass",
	"a=ice-ufrag:gofabricufrag",
	"a=ice-pwd:gofabricicepasswordgofabric",
	"a=fingerprint:" + cameraGoFingerprint,
	"a=rtpmap:96 H264/90000",
	"a=fmtp:96 max-fs=8160",
	"",
}, "\r\n")

// cameraGoCandidate is the DUT's ICE candidate: a host candidate on a
// documentation address (RFC 5737), which no check reaches, so the
// camera's ICE agent keeps checking and ends nothing before Stop.
var cameraGoCandidate = wpdef.ICECandidateStruct{
	Candidate:     "candidate:1 1 UDP 2122260223 192.0.2.10 50000 typ host",
	SdpMid:        spec.Nullable[string]{Value: "0"},
	SdpmLineIndex: spec.Nullable[uint16]{Value: 0},
}

// cameraApp is the running chip-camera-app container.
type cameraApp struct {
	name  string
	image string
	port  int
	// how says which image it runs from and why.
	how string
}

// cameraRun is the case's state across its steps.
type cameraRun struct {
	app   *cameraApp
	ctrl  *gocontroller.Controller
	n     *client.Node
	svc   *camera.Service
	ep    uint16
	first *camera.Session
	video camera.Lease
}

// caseCameraGo is the acceptance: the camera-app commissioned by the DUT,
// one session from offer to Stop, and a second start on the same stream.
func caseCameraGo() goCertCase {
	r := &cameraRun{}
	return goCertCase{
		tc: "CAMERA-GO-1.1", role: roleDUT, origin: cameraGoOrigin, noDaemon: true, finalize: r.finalize,
		steps: []goStep{
			{n: "1", title: "TH_SERVER chip-camera-app starts on its test video source and serves Matter over TCP", run: r.start},
			{n: "2", title: "DUT commissions the camera with outgoing TCP and starts the camera service", run: r.commission},
			{n: "3", title: "DUT starts a stream with an SDP offer: ProvideOffer over TCP, the camera stores RootNodeID", run: r.startFirst},
			{n: "4", title: "The camera's Answer arrives on Session.Signals", run: r.answer},
			{n: "5", title: "DUT sends ProvideICECandidates; the camera's ICECandidates arrive on Session.Signals", run: r.candidates},
			{n: "6", title: "DUT stops the session; the camera's CurrentSessions is empty", run: r.stopFirst},
			{n: "7", title: "DUT starts a second stream, which reuses the allocated video stream, and stops it", run: r.second},
		},
	}
}

// --- step 1: the camera-app ------------------------------------------------------

func (r *cameraRun) start(th *goTH) error {
	ctx, cancel := context.WithTimeout(th.ctx, 10*time.Minute)
	defer cancel()
	app, err := startCameraApp(ctx, th)
	if err != nil {
		return err
	}
	r.app = app
	var listening string
	if !waitFor(th.ctx, 60*time.Second, func() bool {
		log := app.log(th.ctx)
		listening = cameraLogLine(log, "TCP server listening on port "+strconv.Itoa(app.port))
		return listening != "" && strings.Contains(log, "Server Listening...")
	}) {
		return fmt.Errorf("chip-camera-app logged no TCP listener on port %d and no \"Server Listening...\"; its last lines:\n%s", app.port, tail(app.log(th.ctx), 40))
	}
	if !portListening(app.port) {
		return fmt.Errorf("chip-camera-app logged %q, but no TCP socket listens on port %d of the host", listening, app.port)
	}
	th.note("%s; the app logged %q and the host's TCP port %d accepts", app.how, listening, app.port)
	return nil
}

// startCameraApp runs chip-camera-app in a container of the harness
// image. A binary that does not load there fails the step with ldd's
// missing libraries rather than a container that exits unexplained.
func startCameraApp(ctx context.Context, th *goTH) (*cameraApp, error) {
	image, err := pinnedChipImage()
	if err != nil {
		return nil, err
	}
	if err := ensureImage(ctx, image); err != nil {
		return nil, err
	}
	how := "chip-camera-app runs from the harness image " + shortDigest(image)
	missing, err := dockerSh(ctx, image, "ldd /bin/chip-camera-app | grep 'not found' || true")
	if err != nil {
		return nil, fmt.Errorf("ldd chip-camera-app in %s: %w", image, err)
	}
	if missing = strings.TrimSpace(missing); missing != "" {
		return nil, fmt.Errorf("the harness image %s carries chip-camera-app but cannot load it: %s", shortDigest(image), strings.Join(strings.Fields(missing), " "))
	}
	port, err := freeMatterPort()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("gofabric-camera-%d", os.Getpid())
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run() //nolint:gosec // our own container
	args := []string{
		"run", "-d", "--rm", "--name", name,
		"--network", "host",
		"--security-opt", "apparmor=unconfined",
	}
	if _, err := os.Stat("/run/dbus/system_bus_socket"); err == nil {
		args = append(args, "-v", "/run/dbus:/run/dbus")
	}
	args = append(args, "--entrypoint", "/bin/chip-camera-app", image,
		"--KVS", "/tmp/gofabric-camera-kvs",
		"--secured-device-port", strconv.Itoa(port),
		"--discriminator", strconv.Itoa(cameraGoDiscriminator),
		"--passcode", strconv.Itoa(cameraGoPasscode),
		"--camera-test-videosrc",
	)
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil { //nolint:gosec // fixed argv
		return nil, fmt.Errorf("docker run chip-camera-app: %w\n%s", err, out)
	}
	app := &cameraApp{name: name, image: image, port: port, how: how}
	th.t.Cleanup(app.stop)
	return app, nil
}

// ensureImage pulls img unless it is local.
func ensureImage(ctx context.Context, img string) error {
	if exec.CommandContext(ctx, "docker", "image", "inspect", img).Run() == nil { //nolint:gosec // a pinned image
		return nil
	}
	if out, err := exec.CommandContext(ctx, "docker", "pull", img).CombinedOutput(); err != nil { //nolint:gosec // a pinned image
		return fmt.Errorf("docker pull %s: %w\n%s", img, err, out)
	}
	return nil
}

// dockerSh runs a shell line in a throwaway container of img.
func dockerSh(ctx context.Context, img, line string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "sh", img, "-c", line).CombinedOutput() //nolint:gosec // the suite's own line
	return string(out), err
}

// shortDigest is an image reference cut to its digest's first twelve
// characters.
func shortDigest(img string) string {
	if _, d, ok := strings.Cut(img, "@sha256:"); ok && len(d) >= 12 {
		return d[:12]
	}
	return img
}

// freeMatterPort is a port free for both UDP and TCP on the host.
func freeMatterPort() (int, error) {
	for range 20 {
		pc, err := net.ListenPacket("udp6", "[::]:0")
		if err != nil {
			return 0, err
		}
		port := pc.LocalAddr().(*net.UDPAddr).Port
		_ = pc.Close()
		l, err := net.Listen("tcp6", net.JoinHostPort("::", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_ = l.Close()
		return port, nil
	}
	return 0, errors.New("no port free for both UDP and TCP")
}

// portListening reports whether a TCP connection to the port on the
// host's loopback is accepted.
func portListening(port int) bool {
	c, err := net.DialTimeout("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// log is the app's output so far, without colour codes.
func (a *cameraApp) log(ctx context.Context) string {
	out, _ := exec.CommandContext(ctx, "docker", "logs", a.name).CombinedOutput() //nolint:gosec // our own container
	return stripANSI(string(out))
}

func (a *cameraApp) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", a.name).Run() //nolint:gosec // our own container
}

// cameraLogLine is the first line of log holding s, its chip prefix
// ("[time] [pid:tid] [CAM] ") cut, or "".
func cameraLogLine(log, s string) string {
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, s) {
			return chipLogText(l)
		}
	}
	return ""
}

// cameraLogLines are the lines of log holding s, prefixes cut.
func cameraLogLines(log, s string) []string {
	var out []string
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, s) {
			out = append(out, chipLogText(l))
		}
	}
	return out
}

var reChipLogPrefix = regexp.MustCompile(`^\[[0-9.]+\] \[[0-9:]+\] \[[A-Z]+\] `)

func chipLogText(l string) string {
	return strings.TrimSpace(reChipLogPrefix.ReplaceAllString(strings.TrimSpace(l), ""))
}

// since is the part of the app's log after the first offset bytes.
func (a *cameraApp) since(ctx context.Context, offset int) string {
	log := a.log(ctx)
	if offset > len(log) {
		return ""
	}
	return log[offset:]
}

// waitLog waits for a line holding s in the app's log after offset and
// returns it.
func (a *cameraApp) waitLog(th *goTH, offset int, d time.Duration, s string) (string, error) {
	var line string
	if !waitFor(th.ctx, d, func() bool {
		line = cameraLogLine(a.since(th.ctx, offset), s)
		return line != ""
	}) {
		return "", fmt.Errorf("chip-camera-app logged no %q within %v; its last lines:\n%s", s, d, tail(a.since(th.ctx, offset), 40))
	}
	return line, nil
}

// --- step 2: commissioning and the service -------------------------------------

// commission builds the DUT as matterjs-server builds its controller for
// cameras — TCP on and preferred (packages/ws-controller/src/controller/ControllerNode.ts:113-114
// at 0bd06f2) — with the camera requestor (Config.Camera) and the
// controller's own advertiser (nil: mdns.NewResponder()), which publishes
// RootNodeID's operational record for a camera that calls back over CASE.
func (r *cameraRun) commission(th *goTH) error {
	if r.app == nil {
		return errors.New("step 1 started no camera-app")
	}
	var reports camera.Reports
	ctrl, err := th.controllerWith("dut", func(c *gocontroller.Config) {
		c.Camera = &gocontroller.CameraConfig{}
		c.TCP = gocontroller.TCPConfig{Outgoing: true}
		c.TransportPreference = exchange.TransportTCP
		reports.Install(c)
	})
	if err != nil {
		return err
	}
	r.ctrl = ctrl
	code, err := setup.ManualCode(cameraGoDiscriminator, cameraGoPasscode)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(th.ctx, 3*time.Minute)
	defer cancel()
	// The default subscription stays on: the service reads the camera from
	// its reports (camera.Reports).
	if r.n, err = ctrl.Pair(ctx, code, gocontroller.PairOptions{OnAttestationFailure: attestation.Accept()}); err != nil {
		return fmt.Errorf("the DUT commissions chip-camera-app: %w", err)
	}
	if !waitFor(th.ctx, 60*time.Second, func() bool {
		for _, e := range r.n.Endpoints() {
			if e.Cluster(wpdef.ClusterID) != nil && e.Cluster(csdef.ClusterID) != nil {
				r.ep = e.Number()
				return true
			}
		}
		return false
	}) {
		return errors.New("the camera's report shows no endpoint with WebRtcTransportProvider and CameraAvStreamManagement")
	}
	if r.svc, err = camera.New(th.ctx, ctrl, camera.Config{Reports: &reports, Logger: th.log}); err != nil {
		return fmt.Errorf("camera.New: %w", err)
	}
	caps, err := r.svc.Capabilities(ctx, r.n, r.ep)
	if err != nil {
		return fmt.Errorf("Capabilities: %w", err)
	}
	addr, _ := r.n.PeerAddress()
	codecs := make([]string, 0, len(caps.Video.Codecs))
	for _, c := range caps.Video.Codecs {
		codecs = append(codecs, map[csdef.VideoCodecEnum]string{csdef.VideoCodecH264: "H.264", csdef.VideoCodecHevc: "HEVC", csdef.VideoCodecVvc: "VVC", csdef.VideoCodecAv1: "AV1"}[c])
	}
	th.note("the DUT (RootNodeID 0x%X) commissioned the camera as node 0x%X; endpoint %d serves the provider; the camera states features %v, video codecs %v, %d allocated video streams, %d sessions",
		ctrl.Fabric().RootNodeID, addr.NodeID, r.ep, caps.Features, codecs, len(caps.Allocated.Video), len(caps.Sessions))
	return nil
}

// --- steps 3 to 7: the session ---------------------------------------------------

// startOffer is one StartStream with the offer, video only: the offer
// carries no audio section.
func (r *cameraRun) startOffer(th *goTH, key string) (*camera.Session, error) {
	ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
	defer cancel()
	offer := cameraGoOffer
	return r.svc.StartStream(ctx, camera.StartRequest{
		Node: r.n, Endpoint: r.ep, Usage: csdef.StreamUsageLiveView, Offer: &offer, DeclineAudio: true, Owner: "acceptance", Key: key,
	})
}

// currentSessions reads the camera's CurrentSessions.
func (r *cameraRun) currentSessions(th *goTH) ([]wpdef.WebRTCSessionStruct, error) {
	v, err := th.readValue(r.n, attrPath(r.ep, wpdef.ClusterID, wpdef.AttrCurrentSessions))
	if err != nil {
		return nil, err
	}
	return spec.DecodeList(spec.DecodeStruct[wpdef.WebRTCSessionStruct])(v)
}

func describeSessionList(ss []wpdef.WebRTCSessionStruct) string {
	if len(ss) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("{id %d peer 0x%X ep %d usage %d}", s.Id, s.PeerNodeId, s.PeerEndpointId, s.StreamUsage))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func (r *cameraRun) startFirst(th *goTH) error {
	if r.svc == nil {
		return errors.New("step 2 started no camera service")
	}
	offset := len(r.app.log(th.ctx))
	s, err := r.startOffer(th, "first")
	if err != nil {
		return fmt.Errorf("StartStream: %w", err)
	}
	r.first = s
	if s.Mode != camera.ModeProvideOffer || len(s.Streams) != 1 || s.Streams[0].Kind != camera.StreamVideo {
		return fmt.Errorf("the session is %+v, want one video stream in provide_offer mode", s)
	}
	r.video = s.Streams[0]
	handled, err := r.app.waitLog(th, offset, 10*time.Second, "HandleProvideOffer called")
	if err != nil {
		return err
	}
	ss, err := r.currentSessions(th)
	if err != nil {
		return err
	}
	root := r.ctrl.Fabric().RootNodeID
	var held *wpdef.WebRTCSessionStruct
	for i := range ss {
		if ss[i].Id == s.ID {
			held = &ss[i]
		}
	}
	if held == nil || held.PeerNodeId != root || held.PeerEndpointId != gocontroller.CameraControllerEndpoint {
		return fmt.Errorf("the camera's CurrentSessions is %s, want session %d with peer RootNodeID 0x%X endpoint %d", describeSessionList(ss), s.ID, root, gocontroller.CameraControllerEndpoint)
	}
	tcp, err := tcpSessionOf((&tcpRun{ctrl: r.ctrl, n: r.n}).sessions())
	if err != nil {
		return fmt.Errorf("ProvideOffer is a large message: %w", err)
	}
	th.note("session %d in %s mode on video stream %d (%s); the DUT holds TCP session %d with the camera (payload up to %d bytes); the camera logged %q and lists %s",
		s.ID, s.Mode, r.video.ID, r.video.Provenance, tcp.ID(), tcp.MaxPayloadSize(), handled, describeSessionList(ss))
	return nil
}

// answerDirection is the direction attribute of the first answer the app
// logged (OnLocalDescription prints the SDP after "Local Description
// (answer):", webrtc-provider-manager.cpp:1141-1142 at 6170af84).
func answerDirection(log string) string {
	i := strings.Index(log, "Local Description (answer):")
	if i < 0 {
		return "no logged answer"
	}
	for _, l := range strings.Split(log[i:], "\n") {
		switch l = strings.TrimSpace(chipLogText(l)); l {
		case "a=sendonly", "a=recvonly", "a=sendrecv", "a=inactive":
			return l
		}
	}
	return "no direction"
}

// nextSignal waits for the session's next signal.
func nextSignal(th *goTH, s *camera.Session, d time.Duration) (camera.Signal, error) {
	ctx, cancel := context.WithTimeout(th.ctx, d)
	defer cancel()
	select {
	case sig, ok := <-s.Signals():
		if !ok {
			return camera.Signal{}, fmt.Errorf("session %d's Signals closed", s.ID)
		}
		return sig, nil
	case <-ctx.Done():
		return camera.Signal{}, fmt.Errorf("no signal of session %d within %v", s.ID, d)
	}
}

func (r *cameraRun) answer(th *goTH) error {
	if r.first == nil {
		return errors.New("step 3 started no session")
	}
	sig, err := nextSignal(th, r.first, 30*time.Second)
	if err != nil {
		return err
	}
	if sig.Kind != camera.SignalAnswer || !strings.Contains(sig.SDP, "m=video") {
		return fmt.Errorf("the first signal is kind %d with SDP %q, want the Answer to the video offer", sig.Kind, sig.SDP)
	}
	mline, dir := "", ""
	for _, l := range strings.Split(sig.SDP, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "m=video") && mline == "":
			mline = l
		case l == "a=sendonly" || l == "a=recvonly" || l == "a=sendrecv" || l == "a=inactive":
			dir = l
		}
	}
	log := r.app.log(th.ctx)
	th.note("the camera's answer, %s, %s in the app's own log, reached the session as %d bytes of SDP with %s (narrowed to the recvonly offer); the camera logged %q and %q",
		mline, answerDirection(log), len(sig.SDP), dir, cameraLogLine(log, "Sending Answer command to node"), cameraLogLine(log, "Answer command succeeds"))
	return nil
}

func (r *cameraRun) candidates(th *goTH) error {
	if r.first == nil {
		return errors.New("step 3 started no session")
	}
	offset := len(r.app.log(th.ctx))
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	if err := r.first.ProvideICECandidates(ctx, []wpdef.ICECandidateStruct{cameraGoCandidate}); err != nil {
		return fmt.Errorf("ProvideICECandidates: %w", err)
	}
	handled, err := r.app.waitLog(th, offset, 10*time.Second, "HandleProvideICECandidates called with session ID "+strconv.Itoa(int(r.first.ID)))
	if err != nil {
		return err
	}
	// The camera-app sends its own candidates once it received the
	// requestor's: "Schedule sending Ice Candidates when remote candidates
	// are received" (examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:611-613
	// at 6170af84).
	candidates, first := 0, ""
	for candidates == 0 {
		sig, err := nextSignal(th, r.first, 20*time.Second)
		if err != nil {
			return fmt.Errorf("after ProvideICECandidates: %w", err)
		}
		if sig.Kind != camera.SignalICECandidates {
			return fmt.Errorf("after ProvideICECandidates the camera sent signal kind %d, want ICECandidates", sig.Kind)
		}
		candidates += len(sig.Candidates)
		if len(sig.Candidates) > 0 {
			first = sig.Candidates[0].Candidate
		}
	}
	log := r.app.since(th.ctx, offset)
	th.note("the camera logged %q and %v; it sent %d ICE candidate(s), the first %q, and logged %q",
		handled, cameraLogLines(log, "Applying candidate"), candidates, first, cameraLogLine(log, "ICECandidates command succeeds"))
	return nil
}

// stop stops s, holds the camera's EndSession line and its empty
// CurrentSessions, and returns the camera's line.
func (r *cameraRun) stop(th *goTH, s *camera.Session) (string, error) {
	offset := len(r.app.log(th.ctx))
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	ended, err := s.Stop(ctx)
	if err != nil {
		return "", fmt.Errorf("Stop: %w", err)
	}
	if !ended {
		return "", fmt.Errorf("Stop: the camera no longer held session %d", s.ID)
	}
	line, err := r.app.waitLog(th, offset, 10*time.Second, "EndSession for session: "+strconv.Itoa(int(s.ID)))
	if err != nil {
		return "", err
	}
	var ss []wpdef.WebRTCSessionStruct
	if !waitFor(th.ctx, 10*time.Second, func() bool {
		ss, err = r.currentSessions(th)
		return err == nil && len(ss) == 0
	}) {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("the camera's CurrentSessions is %s after Stop, want []", describeSessionList(ss))
	}
	if err := signalsClosed(th, s); err != nil {
		return "", err
	}
	return line, nil
}

// signalsClosed drains what the camera sent before Stop and waits for the
// session's Signals to close.
func signalsClosed(th *goTH, s *camera.Session) error {
	ctx, cancel := context.WithTimeout(th.ctx, 10*time.Second)
	defer cancel()
	for {
		select {
		case _, open := <-s.Signals():
			if !open {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("session %d's Signals stay open after Stop", s.ID)
		}
	}
}

func (r *cameraRun) stopFirst(th *goTH) error {
	if r.first == nil {
		return errors.New("step 3 started no session")
	}
	line, err := r.stop(th, r.first)
	if err != nil {
		return err
	}
	th.note("Stop ended session %d; the camera logged %q, its CurrentSessions reads [] and the session's Signals closed", r.first.ID, line)
	return nil
}

func (r *cameraRun) second(th *goTH) error {
	if r.first == nil {
		return errors.New("step 3 started no session")
	}
	s, err := r.startOffer(th, "second")
	if err != nil {
		return fmt.Errorf("StartStream: %w", err)
	}
	if len(s.Streams) != 1 || s.Streams[0].Kind != camera.StreamVideo {
		return fmt.Errorf("the second session is %+v, want one video stream", s)
	}
	v := s.Streams[0]
	if v.ID != r.video.ID || v.Provenance == camera.ProvenanceAllocated {
		return fmt.Errorf("the second session's video stream is %d (%s), want the first's %d reused", v.ID, v.Provenance, r.video.ID)
	}
	ss, err := r.currentSessions(th)
	if err != nil {
		return err
	}
	if len(ss) != 1 || ss[0].Id != s.ID {
		return fmt.Errorf("the camera's CurrentSessions is %s, want the second session %d alone", describeSessionList(ss), s.ID)
	}
	sig, err := nextSignal(th, s, 30*time.Second)
	if err != nil {
		return fmt.Errorf("the second session's Answer: %w", err)
	}
	if sig.Kind != camera.SignalAnswer {
		return fmt.Errorf("the second session's first signal is kind %d, want the Answer", sig.Kind)
	}
	line, err := r.stop(th, s)
	if err != nil {
		return err
	}
	th.note("session %d reuses video stream %d (%s); the camera answered it, and Stop ended it: the camera logged %q and its CurrentSessions reads []", s.ID, v.ID, v.Provenance, line)
	return nil
}

func (r *cameraRun) finalize(th *goTH) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if r.svc != nil {
		_ = r.svc.Close(ctx)
	}
	if r.n != nil {
		_ = r.ctrl.Registry().Decommission(ctx, r.n)
	}
	if r.app != nil {
		r.app.stop()
	}
}
