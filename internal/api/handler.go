package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"rtpengine-mon/internal/rtpengine"
	"rtpengine-mon/internal/spy"
)

type Handler struct {
	rtpClient  rtpengine.Client
	spyService *spy.Service
	tracer     trace.Tracer
}

func NewHandler(rtpClient rtpengine.Client, spyService *spy.Service) *Handler {
	return &Handler{
		rtpClient:  rtpClient,
		spyService: spyService,
		tracer:     otel.Tracer("http-handler"),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/calls", h.handleListCalls)
	mux.HandleFunc("/calls/", h.handleCallDetails)
	mux.HandleFunc("/spy/", h.handleSpy)
	mux.HandleFunc("/spy/answer/", h.handleSpyAnswer)
	mux.HandleFunc("/stats", h.handleStatistics)
}

func (h *Handler) handleListCalls(w http.ResponseWriter, r *http.Request) {
	ctx, span := h.tracer.Start(r.Context(), "http.ListCalls", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	list, err := h.rtpClient.ListCalls(ctx)
	if err != nil {
		h.respondError(w, err, http.StatusInternalServerError)
		return
	}
	h.respondJSON(w, list)
}

func (h *Handler) handleCallDetails(w http.ResponseWriter, r *http.Request) {
	callID := r.URL.Path[len("/calls/"):]
	if callID == "" {
		h.respondError(w, fmt.Errorf("call ID required"), http.StatusBadRequest)
		return
	}

	ctx, span := h.tracer.Start(r.Context(), "http.CallDetails", trace.WithAttributes(attribute.String("call_id", callID)), trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	details, err := h.rtpClient.QueryCall(ctx, callID)
	if err != nil {
		h.respondError(w, err, http.StatusInternalServerError)
		return
	}
	h.respondJSON(w, details)
}

type SpyRequest struct {
	FromTag string `json:"from_tag"`
	ToTag   string `json:"to_tag"`
}
type SpyResponse struct {
	SpyID   string `json:"spyID"`
	SDP     string `json:"sdp"`
	FromTag string `json:"from_tag"`
	ToTag   string `json:"to_tag"`
}

func (h *Handler) handleSpy(w http.ResponseWriter, r *http.Request) {
	callID := r.URL.Path[len("/spy/"):]
	
	ctx, span := h.tracer.Start(r.Context(), "http.Spy", trace.WithAttributes(attribute.String("call_id", callID)),  trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	var req SpyRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	sessionID, sdp, fromTag, toTag, err := h.spyService.StartSpySession(ctx, callID, req.FromTag, req.ToTag)
	if err != nil {
		h.respondError(w, err, http.StatusInternalServerError)
		return
	}

	h.respondJSON(w, SpyResponse{
		SpyID:   sessionID,
		SDP:     sdp,
		FromTag: fromTag,
		ToTag:   toTag,
	})
}

func (h *Handler) handleSpyAnswer(w http.ResponseWriter, r *http.Request) {
	spyID := r.URL.Path[len("/spy/answer/"):]
	
	ctx, span := h.tracer.Start(r.Context(), "http.SpyAnswer", trace.WithAttributes(attribute.String("spy_id", spyID)), trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	var msg struct {
		SDP string `json:"sdp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		h.respondError(w, err, http.StatusBadRequest)
		return
	}

	if err := h.spyService.HandleSpyAnswer(ctx, spyID, msg.SDP); err != nil {
		h.respondError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleStatistics(w http.ResponseWriter, r *http.Request) {
	ctx, span := h.tracer.Start(r.Context(), "http.Statistics", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	stats, err := h.rtpClient.Statistics(ctx)
	if err != nil {
		h.respondError(w, err, http.StatusInternalServerError)
		return
	}
	h.respondJSON(w, stats)
}

func (h *Handler) respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fmt.Printf("Error encoding JSON: %v", err)
	}
}

func (h *Handler) respondError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
