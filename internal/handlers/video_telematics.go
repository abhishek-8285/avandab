package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/videotelematics"
)

type VideoTelematicsHandlers struct {
	*App
}

func NewVideoTelematicsHandlers(app *App) *VideoTelematicsHandlers {
	return &VideoTelematicsHandlers{App: app}
}

func (h *VideoTelematicsHandlers) service() *videotelematics.Service {
	mediaServerURL := ""
	if h.Config != nil {
		mediaServerURL = h.Config.Video.MediaServerURL
	}
	return videotelematics.NewService(videotelematics.NewSQLRepository(h.DB), mediaServerURL)
}

func writeVideoJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeVideoError(w http.ResponseWriter, code int, msg string) {
	writeVideoJSON(w, code, map[string]string{"error": msg})
}

// RegisterAPIRoutes mounts the RESTful Video Telematics endpoints at /api/v1/video.
func (h *VideoTelematicsHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/video", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/cameras", h.APIListCameras)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/cameras/{id}", h.APIGetCamera)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "write")).Post("/cameras/{id}/profile", h.APIConfigureProfile)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "write")).Post("/events", h.APIIngestEvent)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/events", h.APIListEvents)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/events/{id}", h.APIGetEvent)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "write")).Post("/events/{id}/review", h.APIReviewEvent)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Post("/livestream/start", h.APIStartLivestream)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Post("/livestream/{id}/stop", h.APIEndLivestream)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/drivers/{id}/scorecard", h.APIGetDriverScorecard)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/scorecards", h.APIListDriverScorecards)
		r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/metrics", h.APIGetFleetMetrics)
	})
}

// Routes mounts the Video Telematics web UI handlers at /video.
func (h *VideoTelematicsHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Get("/", h.WebVideoDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "video", "write")).Post("/cameras", h.WebConfigureProfile)
	r.With(middleware.RequirePermission(h.AuthSrv, "video", "write")).Post("/events/{id}/review", h.WebReviewEvent)
	r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Post("/livestream/start", h.WebStartLivestream)
	r.With(middleware.RequirePermission(h.AuthSrv, "video", "read")).Post("/livestream/{id}/stop", h.WebEndLivestream)
}

// APIListCameras returns all dashcam-equipped vehicles.
func (h *VideoTelematicsHandlers) APIListCameras(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	vehicles, err := h.service().ListDashcamVehicles(r.Context(), tenantID)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, map[string]interface{}{
		"cameras": vehicles,
		"total":   len(vehicles),
	})
}

// APIGetCamera returns profile and status for a single vehicle dashcam.
func (h *VideoTelematicsHandlers) APIGetCamera(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "id")

	profile, err := h.service().GetProfile(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if profile == nil {
		writeVideoError(w, http.StatusNotFound, "dashcam profile not found")
		return
	}

	writeVideoJSON(w, http.StatusOK, profile)
}

// APIConfigureProfile creates or updates dashcam hardware bindings and AI vision flags.
func (h *VideoTelematicsHandlers) APIConfigureProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "id")

	var in videotelematics.ConfigureDashcamProfileInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeVideoError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	profile, err := h.service().ConfigureProfile(r.Context(), tenantID, vehicleID, in)
	if err != nil {
		writeVideoError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, profile)
}

// APIIngestEvent receives an edge AI incident from the dashcam device.
func (h *VideoTelematicsHandlers) APIIngestEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in videotelematics.IngestSafetyEventInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeVideoError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	event, err := h.service().IngestSafetyEvent(r.Context(), tenantID, in)
	if err != nil {
		writeVideoError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusCreated, event)
}

// APIListEvents queries video safety events with optional filtering.
func (h *VideoTelematicsHandlers) APIListEvents(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	filter := videotelematics.EventFilter{
		VehicleID:    r.URL.Query().Get("vehicle_id"),
		DriverID:     r.URL.Query().Get("driver_id"),
		Category:     r.URL.Query().Get("category"),
		EventType:    r.URL.Query().Get("event_type"),
		Severity:     r.URL.Query().Get("severity"),
		ReviewStatus: r.URL.Query().Get("review_status"),
		Limit:        limit,
		Offset:       offset,
	}

	events, total, err := h.service().ListSafetyEvents(r.Context(), tenantID, filter)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
	})
}

// APIGetEvent returns safety incident details by ID.
func (h *VideoTelematicsHandlers) APIGetEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	eventID := chi.URLParam(r, "id")

	event, err := h.service().GetSafetyEvent(r.Context(), tenantID, eventID)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if event == nil {
		writeVideoError(w, http.StatusNotFound, "safety event not found")
		return
	}

	writeVideoJSON(w, http.StatusOK, event)
}

// APIReviewEvent updates review status with coaching notes.
func (h *VideoTelematicsHandlers) APIReviewEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	eventID := chi.URLParam(r, "id")

	var in videotelematics.ReviewSafetyEventInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeVideoError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	user, _ := h.getUserFromContext(r)
	if in.ReviewedBy == "" && user != nil && user.UserID != "" {
		in.ReviewedBy = user.UserID
	}

	event, err := h.service().ReviewSafetyEvent(r.Context(), tenantID, eventID, in)
	if err != nil {
		writeVideoError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, event)
}

// APIStartLivestream initiates a live video streaming session with a vehicle camera.
func (h *VideoTelematicsHandlers) APIStartLivestream(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in videotelematics.StartLivestreamInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeVideoError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	user, _ := h.getUserFromContext(r)
	if in.RequestedBy == "" && user != nil && user.UserID != "" {
		in.RequestedBy = user.UserID
	}

	session, err := h.service().StartLivestream(r.Context(), tenantID, in)
	if err != nil {
		writeVideoError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, session)
}

// APIEndLivestream concludes an active video stream session.
func (h *VideoTelematicsHandlers) APIEndLivestream(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	sessionID := chi.URLParam(r, "id")

	if err := h.service().EndLivestream(r.Context(), tenantID, sessionID); err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

// APIGetDriverScorecard returns safety scorecard for a driver.
func (h *VideoTelematicsHandlers) APIGetDriverScorecard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	driverID := chi.URLParam(r, "id")

	card, err := h.service().GetDriverScorecard(r.Context(), tenantID, driverID)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if card == nil {
		// Calculate on the fly if not yet generated
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -30)
		card, err = h.service().CalculateAndSaveDriverScorecard(r.Context(), tenantID, driverID, start, now, 0)
		if err != nil {
			writeVideoError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeVideoJSON(w, http.StatusOK, card)
}

// APIListDriverScorecards returns all driver scorecards for the fleet leaderboard.
func (h *VideoTelematicsHandlers) APIListDriverScorecards(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	cards, total, err := h.service().ListDriverScorecards(r.Context(), tenantID, limit, offset)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, map[string]interface{}{
		"scorecards": cards,
		"total":      total,
	})
}

// APIGetFleetMetrics returns top-level safety KPIs across all cameras.
func (h *VideoTelematicsHandlers) APIGetFleetMetrics(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	metrics, err := h.service().GetFleetMetrics(r.Context(), tenantID)
	if err != nil {
		writeVideoError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeVideoJSON(w, http.StatusOK, metrics)
}

// WebVideoDashboard renders the main AI Dashcam cockpit.
func (h *VideoTelematicsHandlers) WebVideoDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	metrics, err := h.service().GetFleetMetrics(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load Video metrics", err.Error(), user)
		return
	}

	vehicles, err := h.service().ListDashcamVehicles(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load Dashcam fleet", err.Error(), user)
		return
	}

	events, _, err := h.service().ListSafetyEvents(r.Context(), tenantID, videotelematics.EventFilter{Limit: 25})
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load safety events", err.Error(), user)
		return
	}

	scorecards, _, err := h.service().ListDriverScorecards(r.Context(), tenantID, 10, 0)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load safety scorecards", err.Error(), user)
		return
	}

	// Fetch all fleet vehicles for the configuration modal dropdown
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, vehicle_number, registration_number
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	var allVehicles []map[string]string
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, vNum, regNum string
			if err := rows.Scan(&id, &vNum, &regNum); err == nil {
				allVehicles = append(allVehicles, map[string]string{
					"ID":                 id,
					"VehicleNumber":      vNum,
					"RegistrationNumber": regNum,
				})
			}
		}
	}

	data := PageData{
		Title: "Video Telematics & AI Safety",
		User:  user,
		Extra: map[string]interface{}{
			"Metrics":     metrics,
			"Vehicles":    vehicles,
			"Events":      events,
			"Scorecards":  scorecards,
			"AllVehicles": allVehicles,
		},
	}

	h.renderPage(w, r, "video_dashboard.html", data)
}

// WebConfigureProfile handles form submission for binding/updating a dashcam profile.
func (h *VideoTelematicsHandlers) WebConfigureProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	vehicleID := strings.TrimSpace(r.FormValue("vehicle_id"))
	if vehicleID == "" {
		h.renderError(w, http.StatusBadRequest, "Vehicle is required", "vehicle_id cannot be blank", user)
		return
	}

	cameraSerial := strings.TrimSpace(r.FormValue("camera_serial"))
	if cameraSerial == "" {
		h.renderError(w, http.StatusBadRequest, "Camera serial is required", "camera_serial cannot be blank", user)
		return
	}

	model := strings.TrimSpace(r.FormValue("camera_make_model"))
	if model == "" {
		model = "FleetX AI Dashcam Pro"
	}

	firmware := strings.TrimSpace(r.FormValue("firmware_version"))
	if firmware == "" {
		firmware = "v2.4.0-edge-ai"
	}

	// Empty status falls through to the service default (STANDBY) — the form
	// must not claim a freshly bound camera is ONLINE (Plan P3 item 3.12).
	status := strings.TrimSpace(r.FormValue("status"))

	dualFacing := r.FormValue("dual_facing") == "1" || r.FormValue("dual_facing") == "true" || r.FormValue("dual_facing") == "on"
	adasEnabled := r.FormValue("adas_enabled") == "1" || r.FormValue("adas_enabled") == "true" || r.FormValue("adas_enabled") == "on"
	dmsEnabled := r.FormValue("dms_enabled") == "1" || r.FormValue("dms_enabled") == "true" || r.FormValue("dms_enabled") == "on"
	liveStream := r.FormValue("live_stream_supported") == "1" || r.FormValue("live_stream_supported") == "true" || r.FormValue("live_stream_supported") == "on"

	in := videotelematics.ConfigureDashcamProfileInput{
		CameraMakeModel:     model,
		CameraSerial:        cameraSerial,
		DualFacing:          dualFacing,
		ADASEnabled:         adasEnabled,
		DMSEnabled:          dmsEnabled,
		LiveStreamSupported: liveStream,
		FirmwareVersion:     firmware,
		Status:              status,
	}

	if _, err := h.service().ConfigureProfile(r.Context(), tenantID, vehicleID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to configure dashcam profile", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/video", http.StatusSeeOther)
}

// WebReviewEvent handles form submission for coaching or dismissing a safety incident.
func (h *VideoTelematicsHandlers) WebReviewEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	eventID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	reviewStatus := strings.TrimSpace(r.FormValue("review_status"))
	if reviewStatus == "" {
		reviewStatus = videotelematics.ReviewCoached
	}

	note := strings.TrimSpace(r.FormValue("coaching_note"))
	if reviewStatus == videotelematics.ReviewCoached && note == "" {
		h.renderError(w, http.StatusBadRequest, "Coaching note required", "Please record feedback provided to the driver", user)
		return
	}

	reviewer := "Fleet Safety Manager"
	if user != nil && user.UserID != "" {
		reviewer = user.UserID
	}

	in := videotelematics.ReviewSafetyEventInput{
		ReviewStatus: reviewStatus,
		CoachingNote: note,
		ReviewedBy:   reviewer,
	}

	if _, err := h.service().ReviewSafetyEvent(r.Context(), tenantID, eventID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to review safety event", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/video", http.StatusSeeOther)
}

// WebStartLivestream initiates a live stream session from the dashboard.
func (h *VideoTelematicsHandlers) WebStartLivestream(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	vehicleID := strings.TrimSpace(r.FormValue("vehicle_id"))
	channel := strings.TrimSpace(r.FormValue("channel"))
	if channel == "" {
		channel = videotelematics.ChannelRoadFacing
	}

	requestedBy := "Operator"
	if user != nil && user.UserID != "" {
		requestedBy = user.UserID
	}

	in := videotelematics.StartLivestreamInput{
		VehicleID:      vehicleID,
		Channel:        channel,
		StreamProtocol: videotelematics.ProtocolWebRTC,
		RequestedBy:    requestedBy,
	}

	if _, err := h.service().StartLivestream(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to start live stream", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/video", http.StatusSeeOther)
}

// WebEndLivestream stops an active streaming session.
func (h *VideoTelematicsHandlers) WebEndLivestream(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	sessionID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := h.service().EndLivestream(r.Context(), tenantID, sessionID); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to end live stream", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/video", http.StatusSeeOther)
}
