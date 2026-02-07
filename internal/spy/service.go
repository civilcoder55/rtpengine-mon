package spy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"sdp-monitor/internal/config"
	"sdp-monitor/internal/rtpengine"
)

// Service provides WebRTC spying capabilities on active RTPEngine calls.
type Service struct {
	cfg       *config.Config
	rtpClient rtpengine.Client
	browserWebrtcAPI *webrtc.API
	backendWebrtcAPI *webrtc.API
	tracer    trace.Tracer
	meter     metric.Meter

	sessionCounter metric.Int64UpDownCounter

	sourcesMu sync.RWMutex
	sources   map[string]*Source

	sessionsMu sync.RWMutex
	sessions   map[string]*Session 
}

func NewService(cfg *config.Config, rtpClient rtpengine.Client, tcpListener net.Listener) (*Service, error) {
	browserWebrtcAPI, err := createBrowserWebRTCApi(cfg, tcpListener)
	if err != nil {
		return nil, fmt.Errorf("failed to create browser WebRTC API: %w", err)
	}

	backendWebrtcAPI, err := createBackendWebRTCApi(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create backend WebRTC API: %w", err)
	}

	tracer := otel.Tracer("spy-service")
	meter := otel.Meter("spy-service")
	sessCounter, _ := meter.Int64UpDownCounter("spy.sessions_active", metric.WithDescription("Number of active browser spy sessions"))

	return &Service{
		cfg:            cfg,
		rtpClient:      rtpClient,
		browserWebrtcAPI: browserWebrtcAPI,
		backendWebrtcAPI: backendWebrtcAPI,
		tracer:         tracer,
		meter:          meter,
		sessionCounter: sessCounter,
		sources:        make(map[string]*Source),
		sessions:       make(map[string]*Session),
	}, nil
}

func createBrowserWebRTCApi(cfg *config.Config, tcpListener net.Listener) (*webrtc.API, error) {
	settingEngine := webrtc.SettingEngine{}
	
	factory := logging.NewDefaultLoggerFactory()
	factory.DefaultLogLevel = logging.LogLevelError
	settingEngine.LoggerFactory = factory

	settingEngine.SetReceiveMTU(8192)

	if tcpListener != nil {
		tcpMux := webrtc.NewICETCPMux(nil, tcpListener, 8)
		settingEngine.SetICETCPMux(tcpMux)
		settingEngine.SetNetworkTypes([]webrtc.NetworkType{
			webrtc.NetworkTypeTCP4,
		})
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register codecs: %w", err)
	}

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithMediaEngine(mediaEngine),
	)
	return api, nil
}

func createBackendWebRTCApi(cfg *config.Config) (*webrtc.API, error) {
	settingEngine := webrtc.SettingEngine{}
	
	factory := logging.NewDefaultLoggerFactory()
	factory.DefaultLogLevel = logging.LogLevelError
	settingEngine.LoggerFactory = factory

	settingEngine.SetNAT1To1IPs(cfg.WebRTCNAT1To1IPs, webrtc.ICECandidateTypeHost)
	settingEngine.SetEphemeralUDPPortRange(cfg.WebRTCMinPort, cfg.WebRTCMaxPort)
	settingEngine.SetReceiveMTU(8192)

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register codecs: %w", err)
	}

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithMediaEngine(mediaEngine),
	)
	return api, nil
}

func (s *Service) StartSpySession(ctx context.Context, callID, fromTag, toTag string) (string, string, string, string, error) {
	ctx, span := s.tracer.Start(ctx, "spy.StartSpySession", trace.WithAttributes(
		attribute.String("call_id", callID),
	))
	defer span.End()

	// 1. Auto-detect tags if missing
	if fromTag == "" || toTag == "" {
		var err error
		fromTag, toTag, err = s.detectTags(ctx, callID)
		if err != nil {
			return "", "", "", "", fmt.Errorf("failed to detect tags: %w", err)
		}
	}

	fmt.Println("Tags for call", callID, ":", fromTag, toTag)

	// 2. Get or Create Source (Backend connection to RTPEngine)
	s.sourcesMu.Lock()
	source, ok := s.sources[callID]
	if !ok {
		var err error
		source, err = s.createSource(ctx, callID, fromTag, toTag)
		if err != nil {
			s.sourcesMu.Unlock()
			return "", "", "", "", fmt.Errorf("failed to create source: %w", err)
		}
		s.sources[callID] = source
	}
	s.sourcesMu.Unlock()

	// 3. Create Spy Session (Connection to Frontend)
	sessionID, offerSDP, err := s.createSession(ctx, source)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to create session: %w", err)
	}

	return sessionID, offerSDP, fromTag, toTag, nil
}

func (s *Service) HandleSpyAnswer(ctx context.Context, sessionID, sdp string) error {
	ctx, span := s.tracer.Start(ctx, "spy.HandleSpyAnswer", trace.WithAttributes(
		attribute.String("session_id", sessionID),
	))
	defer span.End()

	s.sessionsMu.RLock()
	sess, ok := s.sessions[sessionID]
	s.sessionsMu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	err := sess.PC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	return nil
}

func (s *Service) detectTags(ctx context.Context, callID string) (string, string, error) {
	details, err := s.rtpClient.QueryCall(ctx, callID)
	if err != nil {
		return "", "", err
	}

	tagsMap, ok := details["tags"].(map[string]interface{})
	if !ok || len(tagsMap) < 2 {
		return "", "", fmt.Errorf("not enough tags found")
	}

	var tagInfos []TagInfo

	for t, v := range tagsMap {
		var created int64
		if info, ok := v.(map[string]interface{}); ok {
			if c, ok := info["created"].(int64); ok {
				created = c
			} else if c, ok := info["created"].(float64); ok {
				created = int64(c)
			}
		}
		tagInfos = append(tagInfos, TagInfo{Tag: t, Created: created})
	}

	// Sort by creation time
	sort.Slice(tagInfos, func(i, j int) bool {
		if tagInfos[i].Created == tagInfos[j].Created {
			return tagInfos[i].Tag < tagInfos[j].Tag
		}
		return tagInfos[i].Created < tagInfos[j].Created
	})

	return tagInfos[0].Tag, tagInfos[1].Tag, nil
}

func (s *Service) createSource(ctx context.Context, callID, fromTag, toTag string) (*Source, error) {
	ctx, span := s.tracer.Start(ctx, "spy.createSource")
	defer span.End()

	source := NewSource(callID, fromTag, toTag)

	var err error
	// Subscribe to FROM leg (User A)
	source.PCFrom, source.SubTagFrom, err = s.setupBackendSubscription(ctx, callID, fromTag, func(track *webrtc.TrackRemote) {
		var sessionTracks []*webrtc.TrackLocalStaticRTP
		var lastSessionCount int
		
		for {
			select {
			case <-source.ctx.Done():
				return
			default:
				source.mu.RLock()
				currentCount := len(source.Sessions)
				if currentCount != lastSessionCount {
					sessionTracks = make([]*webrtc.TrackLocalStaticRTP, 0, currentCount)
					for _, sess := range source.Sessions {
						sessionTracks = append(sessionTracks, sess.TrackFrom)
					}
					lastSessionCount = currentCount
				}
				source.mu.RUnlock()
				
				rtp, _, readErr := track.ReadRTP()
				if readErr != nil {
					return
				}
				
				for _, t := range sessionTracks {
					if err := t.WriteRTP(rtp); err != nil && err != io.ErrClosedPipe {
						// log error?
					}
				}
			}
		}
	}, func() {
		s.cleanupSource(source)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe from-leg: %w", err)
	}

	// Subscribe to TO leg (User B)
	source.PCTo, source.SubTagTo, err = s.setupBackendSubscription(ctx, callID, toTag, func(track *webrtc.TrackRemote) {
		var sessionTracks []*webrtc.TrackLocalStaticRTP
		var lastSessionCount int
		
		for {
			select {
			case <-source.ctx.Done():
				return
			default:
				source.mu.RLock()
				currentCount := len(source.Sessions)
				if currentCount != lastSessionCount {
					sessionTracks = make([]*webrtc.TrackLocalStaticRTP, 0, currentCount)
					for _, sess := range source.Sessions {
						sessionTracks = append(sessionTracks, sess.TrackTo)
					}
					lastSessionCount = currentCount
				}
				source.mu.RUnlock()
				
				rtp, _, readErr := track.ReadRTP()
				if readErr != nil {
					return
				}
				
				for _, t := range sessionTracks {
					if err := t.WriteRTP(rtp); err != nil && err != io.ErrClosedPipe {
						// log error?
					}
				}
			}
		}
	}, func() {
		s.cleanupSource(source)
	})
	if err != nil {
		s.cleanupSource(source)
		return nil, fmt.Errorf("failed to subscribe to-leg: %w", err)
	}

	return source, nil
}

func (s *Service) setupBackendSubscription(ctx context.Context, callID, tag string, onTrack func(*webrtc.TrackRemote), onClose func()) (*webrtc.PeerConnection, string, error) {
	pc, err := s.backendWebrtcAPI.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", err
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			go onTrack(track)
		}
	})
	
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			onClose()
		}
	})

	resp, err := s.rtpClient.Subscribe(ctx, callID, tag)
	if err != nil {
		pc.Close()
		return nil, "", err
	}

	offerSDP, ok := resp["sdp"].(string)
	if !ok || offerSDP == "" {
		pc.Close()
		return nil, "", fmt.Errorf("invalid SDP from rtpengine")
	}
	subscriptionTag, _ := resp["to-tag"].(string)

	fmt.Println("offerSDP", offerSDP)
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		pc.Close()
		return nil, "", err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return nil, "", err
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return nil, "", err
	}
	<-webrtc.GatheringCompletePromise(pc)

	finalSDP := strings.ReplaceAll(pc.LocalDescription().SDP, "m=audio 0", "m=audio 9")
	fmt.Println("Answer SDP", finalSDP)

	if _, err := s.rtpClient.SubscribeAnswer(ctx, callID, finalSDP, subscriptionTag); err != nil {
		pc.Close()
		return nil, "", err
	}

	return pc, subscriptionTag, nil
}

func (s *Service) createSession(ctx context.Context, source *Source) (string, string, error) {
	pc, err := s.browserWebrtcAPI.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", "", err
	}

	trackFrom, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio_from", "pion")
	if err != nil {
		pc.Close(); return "", "", err
	}
	trackTo, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio_to", "pion")
	if err != nil {
		pc.Close(); return "", "", err
	}

	if _, err = pc.AddTrack(trackFrom); err != nil {
		pc.Close(); return "", "", err
	}
	if _, err = pc.AddTrack(trackTo); err != nil {
		pc.Close(); return "", "", err
	}

	sessionID := uuid.New().String()
	sess := &Session{
		ID:        sessionID,
		PC:        pc,
		TrackFrom: trackFrom,
		TrackTo:   trackTo,
	}

	s.sessionsMu.Lock()
	s.sessions[sessionID] = sess
	s.sessionsMu.Unlock()
	s.sessionCounter.Add(ctx, 1)

	source.mu.Lock()
	source.Sessions[sessionID] = sess
	source.mu.Unlock()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			s.cleanupSession(sessionID, source)
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return "", "", err
	}

	if err := pc.SetLocalDescription(offer); err != nil {
		return "", "", err
	}

	<-webrtc.GatheringCompletePromise(pc)

	return sessionID, pc.LocalDescription().SDP, nil
}

func (s *Service) cleanupSession(sessionID string, source *Source) {
	s.sessionsMu.Lock()
	if _, ok := s.sessions[sessionID]; ok {
		delete(s.sessions, sessionID)
		s.sessionCounter.Add(context.Background(), -1)
	}
	s.sessionsMu.Unlock()

	source.mu.Lock()
	delete(source.Sessions, sessionID)
	// remaining := len(source.Sessions)
	source.mu.Unlock()

	// if remaining == 0 {
	// 	s.cleanupSource(source)
	// }
}

func (s *Service) cleanupSource(source *Source) {
	s.sourcesMu.Lock()
	if _, ok := s.sources[source.CallID]; ok {
		delete(s.sources, source.CallID)
		
		source.cancel()
		
		go func() {
			if source.PCFrom != nil {
				source.PCFrom.Close()
				s.rtpClient.UnSubscribe(context.Background(), source.CallID, source.SubTagFrom)
			}
			if source.PCTo != nil {
				source.PCTo.Close()
				s.rtpClient.UnSubscribe(context.Background(), source.CallID, source.SubTagTo)
			}
		}()
	}
	s.sourcesMu.Unlock()
}
