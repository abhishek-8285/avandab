package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
)

// SettingsHandlers handles company settings management.
type SettingsHandlers struct {
	*App
}

func (h *SettingsHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "settings", "read")).Get("/", h.Index)
	r.With(middleware.ResourcePermission(h.AuthSrv, "settings", "update")).Post("/update", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "settings", "update")).Get("/onboard", h.OnboardPage)
	r.With(middleware.ResourcePermission(h.AuthSrv, "settings", "update")).Post("/onboard", h.SaveOnboard)

	// Per-org feature flags (plugin-style toggles) — settings admins only.
	featuresAdmin := &FeaturesAdmin{App: h.App}
	r.With(middleware.ResourcePermission(h.AuthSrv, "settings", "update")).Get("/features", featuresAdmin.Page)
	r.With(middleware.ResourcePermission(h.AuthSrv, "settings", "update")).Post("/features/toggle", featuresAdmin.Toggle)
}

func (h *SettingsHandlers) OnboardPage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.Role != "admin" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	settings, err := h.Services.Settings.GetSettings(r.Context())
	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}
	h.renderPage(w, r, "company_onboard.html", PageData{
		Title: "Company Onboarding",
		User:  session,
		Extra: map[string]interface{}{"Settings": settings},
	})
}

func (h *SettingsHandlers) SaveOnboard(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	h.Update(w, r)
}

func (h *SettingsHandlers) Index(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	settings, err := h.Services.Settings.GetSettings(r.Context())
	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	h.renderPage(w, r, "settings.html", PageData{
		Title: "Settings",
		User:  session,
		Extra: map[string]interface{}{"Settings": settings},
	})
}

func (h *SettingsHandlers) Update(w http.ResponseWriter, r *http.Request) {
	// company_settings is the PLATFORM-global singleton (Spec 24 §4.7): under
	// multi-tenant mode only the platform administrator may write it — tenant
	// branding flows through the per-tenant overlay instead.
	if h.Config != nil && h.Config.MultiTenant.Enabled {
		session, _ := h.getUserFromContext(r)
		if session == nil || session.Role != string(domain.RoleAdmin) {
			http.Error(w, "platform settings are managed by the platform administrator", http.StatusForbidden)
			return
		}
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Invalid Form Submission")
		return
	}

	// Preserve existing logo if no new file is uploaded
	var logoPath *string
	current, err := h.Services.Settings.GetSettings(r.Context())
	if err == nil && current.LogoPath != nil {
		logoPath = current.LogoPath
	}

	// Handle logo upload
	if file, _, err := r.FormFile("logo"); err == nil {
		defer func() { _ = file.Close() }()

		uploaded, upErr := saveLogo(file, h.Config.UploadDir)
		if upErr != nil {
			h.failPage(w, r, upErr, http.StatusBadRequest, "Logo Upload Failed")
			return
		}
		logoPath = &uploaded
	}

	gstEnabled := r.PostFormValue("gst_enabled") == "1"
	gstRate, _ := parseDecimal(r.PostFormValue("gst_rate"))

	_, err = h.Services.Settings.UpdateSettings(
		r.Context(),
		r.PostFormValue("company_name"),
		r.PostFormValue("currency"),
		r.PostFormValue("timezone"),
		gstEnabled,
		gstRate,
		r.PostFormValue("booking_prefix"),
		r.PostFormValue("trip_prefix"),
		r.PostFormValue("invoice_prefix"),
		r.PostFormValue("financial_year"),
		r.PostFormValue("address"),
		r.PostFormValue("phone"),
		r.PostFormValue("email"),
		r.PostFormValue("gst_number"),
		logoPath,
	)
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Could Not Save Settings")
		return
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// logoMimeExt maps detected MIME types to safe server-generated extensions.
var logoMimeExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// saveLogo writes an uploaded logo file to the uploads directory and returns
// the relative path (from UploadDir) that should be stored in logo_path.
// The file type is validated by magic bytes, never by the client filename.
func saveLogo(file io.Reader, uploadDir string) (string, error) {
	subdir := filepath.Join(uploadDir, "company")
	if err := os.MkdirAll(subdir, 0o750); err != nil {
		return "", err
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	}
	head = head[:n]

	contentType := http.DetectContentType(head)
	if contentType == "image/svg+xml" {
		return "", fmt.Errorf("SVG logo uploads are not allowed")
	}
	ext, ok := logoMimeExt[contentType]
	if !ok {
		return "", fmt.Errorf("unsupported logo file type: %s", contentType)
	}

	filename := uuid.NewString() + ext
	dest := filepath.Join(subdir, filename)

	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, io.MultiReader(bytes.NewReader(head), file)); err != nil {
		return "", err
	}

	return filepath.Join("company", filename), nil
}

func parseDecimal(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
