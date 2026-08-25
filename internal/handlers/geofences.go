package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/geofence/application"
	"transport-app/internal/geofence/domain"
	geofencerepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
)

// GeofenceHandlers powers the geofence CRUD UI (Spec 02 §8). Mounted inside
// the protected web group, gated by geofences:* permissions.
type GeofenceHandlers struct {
	*App
	crudUC    *application.ZoneCRUDUseCase
	adminRepo domain.GeofenceAdminRepository
}

// NewGeofenceHandlers wires the zone CRUD use case against the app DB.
func NewGeofenceHandlers(app *App, db *sql.DB) *GeofenceHandlers {
	repo := geofencerepo.NewGeofenceRepository(db)
	return &GeofenceHandlers{
		App:       app,
		crudUC:    application.NewZoneCRUDUseCase(uow.NewSQLUnitOfWork(db), repo, id.NewUUIDGenerator()),
		adminRepo: repo,
	}
}

// Routes mounts the geofence CRUD routes.
func (h *GeofenceHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "geofences", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "geofences", "create")).Get("/new", h.NewForm)
	r.With(middleware.ResourcePermission(h.AuthSrv, "geofences", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "geofences", "update")).Get("/{id}/edit", h.EditForm)
	r.With(middleware.ResourcePermission(h.AuthSrv, "geofences", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "geofences", "delete")).Post("/{id}/delete", h.Delete)
}

func (h *GeofenceHandlers) tenant(ctx context.Context) shared.TenantID {
	t := shared.TenantIDFromContext(ctx)
	if t == "" {
		return shared.DefaultTenant
	}
	return t
}

// List renders the geofence table (full page or Datastar fragment).
func (h *GeofenceHandlers) List(w http.ResponseWriter, r *http.Request) {
	zones, err := h.adminRepo.ListAll(r.Context(), string(h.tenant(r.Context())))
	if err != nil {
		http.Error(w, "Failed to list geofences", http.StatusInternalServerError)
		return
	}
	session, _ := h.getUserFromContext(r)
	flash := readFlashCookies(r, w)

	if isDatastarRequest(r) {
		h.renderFragment(w, "geofence_row.html", map[string]interface{}{
			"Zones": zones, "User": session,
		})
		return
	}
	h.renderPage(w, r, "geofence_list.html", PageData{
		Title:      "Geofences",
		User:       session,
		FlashError: flash.error, FlashSuccess: flash.success,
		Extra: map[string]interface{}{
			"Zones":        zones,
			"StatusFilter": r.URL.Query().Get("status"),
		},
	})
}

const (
	tmplGeofenceEdit = "geofence_edit.html"
	titleNewGeofence = "New Geofence"
	routeGeofences   = "/geofences"
)

// NewForm renders the drawing form with a blank map.
func (h *GeofenceHandlers) NewForm(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, tmplGeofenceEdit, PageData{
		Title: titleNewGeofence,
		User:  session,
		Extra: map[string]interface{}{"Zone": nil, "MapAssets": true},
	})
}

// Create persists a new zone from the drawing form.
func (h *GeofenceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pz, err := parseZoneForm(r)
	if err != nil {
		h.renderForm(w, r, tmplGeofenceEdit, PageData{
			Title: titleNewGeofence, User: session,
			FlashError: err.Error(),
			Extra:      map[string]interface{}{"Zone": nil, "MapAssets": true},
		})
		return
	}
	createdBy := session.UserID
	_, err = h.crudUC.Create(r.Context(), application.CreateZoneCommand{
		TenantID: h.tenant(r.Context()), Name: pz.name, Kind: pz.kind, Shape: pz.shape,
		CenterLat: pz.centerLat, CenterLng: pz.centerLng, RadiusM: pz.radius,
		Polygon: pz.polygon, RouteName: pz.routeName, Priority: pz.priority,
		CreatedBy: &createdBy,
	})
	if err != nil {
		h.renderForm(w, r, tmplGeofenceEdit, PageData{
			Title: titleNewGeofence, User: session,
			FlashError: err.Error(),
			Extra:      map[string]interface{}{"Zone": nil, "MapAssets": true},
		})
		return
	}
	http.SetCookie(w, flashCookie("flash_success", "Geofence created"))
	http.Redirect(w, r, routeGeofences, http.StatusSeeOther)
}

// EditForm renders the drawing form pre-loaded with the zone.
func (h *GeofenceHandlers) EditForm(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	id := chi.URLParam(r, "id")
	zone, err := h.adminRepo.Find(r.Context(), string(h.tenant(r.Context())), id)
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Not Found", "Geofence not found", session)
		return
	}
	polygonJSON, _ := domain.PolygonJSON(zone.Polygon)
	h.renderForm(w, r, tmplGeofenceEdit, PageData{
		Title: "Edit Geofence",
		User:  session,
		Extra: map[string]interface{}{"Zone": zone, "PolygonJSON": polygonJSON, "MapAssets": true},
	})
}

// Update persists changes to an existing zone.
func (h *GeofenceHandlers) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	pz, err := parseZoneForm(r)
	if err != nil {
		http.SetCookie(w, flashCookie("flash_error", err.Error()))
		http.Redirect(w, r, routeGeofences+"/"+id+"/edit", http.StatusSeeOther)
		return
	}
	err = h.crudUC.Update(r.Context(), application.UpdateZoneCommand{
		TenantID: h.tenant(r.Context()), ID: id, Name: pz.name, Kind: pz.kind, Shape: pz.shape,
		CenterLat: pz.centerLat, CenterLng: pz.centerLng, RadiusM: pz.radius,
		Polygon: pz.polygon, RouteName: pz.routeName, Priority: pz.priority,
	})
	if err != nil {
		http.SetCookie(w, flashCookie("flash_error", "Update failed: "+err.Error()))
		http.Redirect(w, r, routeGeofences+"/"+id+"/edit", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, flashCookie("flash_success", "Geofence updated"))
	http.Redirect(w, r, routeGeofences, http.StatusSeeOther)
}

// Delete soft-deletes the zone (is_active=0).
func (h *GeofenceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.crudUC.SoftDelete(r.Context(), h.tenant(r.Context()), id); err != nil {
		http.SetCookie(w, flashCookie("flash_error", "Delete failed: "+err.Error()))
		http.Redirect(w, r, routeGeofences, http.StatusSeeOther)
		return
	}
	http.SetCookie(w, flashCookie("flash_success", "Geofence deleted"))
	http.Redirect(w, r, routeGeofences, http.StatusSeeOther)
}

// parsedZone holds the drawing form fields shared by create/update.
type parsedZone struct {
	name      string
	kind      string
	shape     string
	centerLat float64
	centerLng float64
	radius    float64
	polygon   []domain.Point
	routeName string
	priority  int
}

// parseZoneForm parses the drawing form into shared zone fields.
func parseZoneForm(r *http.Request) (parsedZone, error) {
	var pz parsedZone
	if err := r.ParseForm(); err != nil {
		return pz, err
	}
	pz.name = strings.TrimSpace(r.PostFormValue("name"))
	pz.kind = strings.TrimSpace(r.PostFormValue("kind"))
	pz.shape = strings.TrimSpace(r.PostFormValue("shape"))
	pz.routeName = strings.TrimSpace(r.PostFormValue("route_name"))
	pz.priority, _ = strconv.Atoi(r.PostFormValue("priority"))

	if pz.shape == domain.ShapeCircle {
		var err error
		pz.centerLat, err = strconv.ParseFloat(r.PostFormValue("center_lat"), 64)
		if err != nil {
			return pz, errors.New("center_lat must be a number")
		}
		pz.centerLng, err = strconv.ParseFloat(r.PostFormValue("center_lng"), 64)
		if err != nil {
			return pz, errors.New("center_lng must be a number")
		}
		pz.radius, err = strconv.ParseFloat(r.PostFormValue("radius_m"), 64)
		if err != nil || pz.radius <= 0 {
			return pz, errors.New("radius_m must be a positive number")
		}
	} else if pz.shape == domain.ShapePolygon {
		raw := strings.TrimSpace(r.PostFormValue("polygon"))
		if raw == "" {
			return pz, errors.New("polygon vertices are required — draw the polygon on the map")
		}
		pts, err := domain.PolygonFromJSON(raw)
		if err != nil {
			return pz, errors.New("polygon JSON is invalid: " + err.Error())
		}
		pz.polygon = pts
	} else {
		return pz, errors.New("invalid shape")
	}
	return pz, nil
}
