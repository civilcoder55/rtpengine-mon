package spy

import (
	"context"
	"sync"

	"github.com/pion/webrtc/v4"
)

// Session represents a single browser spying on a call
type Session struct {
	ID        string
	PC        *webrtc.PeerConnection
	TrackFrom *webrtc.TrackLocalStaticRTP
	TrackTo   *webrtc.TrackLocalStaticRTP
}

// Source manages the backend connections to RTPEngine for a specific call
type Source struct {
	CallID     string
	FromTag    string
	ToTag      string
	PCFrom     *webrtc.PeerConnection
	PCTo       *webrtc.PeerConnection
	SubTagFrom string
	SubTagTo   string

	mu       sync.RWMutex
	Sessions map[string]*Session
	
	ctx    context.Context
	cancel context.CancelFunc
}

type TagInfo struct {
	Tag     string
	Created int64
}

func NewSource(callID, fromTag, toTag string) *Source {
	ctx, cancel := context.WithCancel(context.Background())
	return &Source{
		CallID:   callID,
		FromTag:  fromTag,
		ToTag:    toTag,
		Sessions: make(map[string]*Session),
		ctx:      ctx,
		cancel:   cancel,
	}
}
