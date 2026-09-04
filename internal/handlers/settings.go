package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/domain"
	driverapp "transport-app/internal/driver/application"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
	"transport-app/internal/validate"
	vehicleapp "transport-app/internal/vehicle/application"
	vehicleagg "transport-app/internal/vehicle/domain/aggregate"
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

	// Per-org feature flags (plugin-style toggles) — platform admins only.
	// Gated on features:update (admin-only): an org_admin must not enable or
	// disable features, not even for their own org. Feature grants are a
	// platform commercial decision; org admins see only the resulting product.
	featuresAdmin := &FeaturesAdmin{App: h.App}
	r.With(middleware.ResourcePermission(h.AuthSrv, "features", "update")).Get("/features", featuresAdmin.Page)
	r.With(middleware.ResourcePermission(h.AuthSrv, "features", "update")).Post("/features/toggle", featuresAdmin.Toggle)
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

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		if err := r.ParseForm(); err != nil {
			h.failPage(w, r, err, http.StatusBadRequest, "Invalid Form Submission")
			return
		}
	}

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

	companyName := strings.TrimSpace(r.PostFormValue("company_name"))
	if companyName == "" {
		h.failPage(w, r, fmt.Errorf("company name is required"), http.StatusBadRequest, "Company Name Required")
		return
	}

	address := strings.TrimSpace(r.PostFormValue("address"))
	if address == "" {
		h.failPage(w, r, fmt.Errorf("company address is required"), http.StatusBadRequest, "Address Required")
		return
	}

	phone := strings.TrimSpace(r.PostFormValue("phone"))
	if phone == "" {
		h.failPage(w, r, fmt.Errorf("official phone number is required"), http.StatusBadRequest, "Phone Number Required")
		return
	}

	email := strings.TrimSpace(r.PostFormValue("email"))
	if email == "" {
		h.failPage(w, r, fmt.Errorf("contact email is required"), http.StatusBadRequest, "Email Required")
		return
	}

	gstNumber := strings.TrimSpace(r.PostFormValue("gst_number"))
	if gstNumber == "" {
		gstNumber = strings.TrimSpace(r.PostFormValue("gstin"))
	}
	if gstNumber != "" {
		gstNumber = strings.ToUpper(gstNumber)
		if !validate.ValidGSTIN(gstNumber) {
			h.failPage(w, r, fmt.Errorf("invalid GSTIN format: must be 15-character alphanumeric (e.g. 27ABCDE1234F1Z5)"), http.StatusBadRequest, "Invalid GST Number")
			return
		}
	}

	panNumber := strings.TrimSpace(r.PostFormValue("pan_number"))
	if panNumber == "" {
		panNumber = strings.TrimSpace(r.PostFormValue("pan"))
	}
	if panNumber != "" {
		panNumber = strings.ToUpper(panNumber)
		if !validate.ValidPAN(panNumber) {
			h.failPage(w, r, fmt.Errorf("invalid PAN format: must be 10-character alphanumeric (e.g. ABCDE1234F)"), http.StatusBadRequest, "Invalid PAN Number")
			return
		}
	}
	if gstNumber != "" && len(gstNumber) == 15 && panNumber == "" {
		panNumber = gstNumber[2:12]
	}

	currency := strings.TrimSpace(r.PostFormValue("currency"))
	if currency == "" {
		if current.Currency != "" {
			currency = current.Currency
		} else {
			currency = "INR"
		}
	}

	timezone := strings.TrimSpace(r.PostFormValue("timezone"))
	if timezone == "" {
		if current.Timezone != "" {
			timezone = current.Timezone
		} else {
			timezone = "Asia/Kolkata"
		}
	}

	gstEnabled := r.PostFormValue("gst_enabled") == "1" || (gstNumber != "" && r.PostFormValue("gst_enabled") != "0")
	gstRate, _ := parseDecimal(r.PostFormValue("gst_rate"))
	if gstEnabled && gstRate <= 0 {
		gstRate = 18.0
	}

	bookingPrefix := strings.TrimSpace(r.PostFormValue("booking_prefix"))
	if bookingPrefix == "" {
		if current.BookingPrefix != "" {
			bookingPrefix = current.BookingPrefix
		} else {
			bookingPrefix = "BK"
		}
	}
	tripPrefix := strings.TrimSpace(r.PostFormValue("trip_prefix"))
	if tripPrefix == "" {
		if current.TripPrefix != "" {
			tripPrefix = current.TripPrefix
		} else {
			tripPrefix = "TRIP"
		}
	}
	invoicePrefix := strings.TrimSpace(r.PostFormValue("invoice_prefix"))
	if invoicePrefix == "" {
		if current.InvoicePrefix != "" {
			invoicePrefix = current.InvoicePrefix
		} else {
			invoicePrefix = "INV"
		}
	}
	financialYear := strings.TrimSpace(r.PostFormValue("financial_year"))
	if financialYear == "" {
		if current.FinancialYear != nil && *current.FinancialYear != "" {
			financialYear = *current.FinancialYear
		} else {
			financialYear = "2026-27"
		}
	}

	_, err = h.Services.Settings.UpdateSettings(
		r.Context(),
		companyName,
		currency,
		timezone,
		gstEnabled,
		gstRate,
		bookingPrefix,
		tripPrefix,
		invoicePrefix,
		financialYear,
		address,
		phone,
		email,
		gstNumber,
		panNumber,
		logoPath,
	)
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Could Not Save Settings")
		return
	}

	tenantID := shared.TenantIDFromContext(r.Context())
	vehicleAdded := false

	// Optional Step 2: First Vehicle
	regNum := strings.TrimSpace(r.PostFormValue("vehicle_registration"))
	if regNum == "" {
		regNum = strings.TrimSpace(r.PostFormValue("registration_number"))
	}
	if regNum != "" {
		vehicleModel := strings.TrimSpace(r.PostFormValue("vehicle_model"))
		vehicleNum := strings.TrimSpace(r.PostFormValue("vehicle_number"))
		if vehicleNum == "" {
			if vehicleModel != "" {
				vehicleNum = vehicleModel
			} else {
				vehicleNum = regNum
			}
		}

		vType := strings.TrimSpace(r.PostFormValue("vehicle_type"))
		if vType == "" {
			vType = string(vehicleagg.VehicleTypeTruck)
		}
		fuelType := strings.TrimSpace(r.PostFormValue("fuel_type"))
		if fuelType == "" {
			fuelType = string(vehicleagg.FuelTypeDiesel)
		}
		capacity, _ := strconv.ParseInt(r.PostFormValue("capacity"), 10, 64)
		if capacity <= 0 {
			capacity = 1000
		}

		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()
		createVehicleUC := vehicleapp.NewCreateVehicleUseCase(uowImpl, idGenImpl, clockImpl)

		_, vErr := createVehicleUC.Execute(r.Context(), vehicleapp.CreateVehicleCommand{
			TenantID:           tenantID,
			RegistrationNumber: regNum,
			VehicleNumber:      vehicleNum,
			VehicleType:        vehicleagg.VehicleType(vType),
			Capacity:           capacity,
			FuelType:           vehicleagg.FuelType(fuelType),
			InsuranceExpiry:    time.Now().AddDate(1, 0, 0),
			FitnessExpiry:      time.Now().AddDate(1, 0, 0),
			PermitExpiry:       time.Now().AddDate(1, 0, 0),
		})
		if vErr == nil {
			vehicleAdded = true
		}
	}

	// Optional Step 3: First Driver
	driverName := strings.TrimSpace(r.PostFormValue("driver_name"))
	driverPhone := strings.TrimSpace(r.PostFormValue("driver_phone"))
	driverLicense := strings.TrimSpace(r.PostFormValue("driver_license"))
	if driverLicense == "" {
		driverLicense = strings.TrimSpace(r.PostFormValue("license_number"))
	}
	firstName := strings.TrimSpace(r.PostFormValue("first_name"))
	lastName := strings.TrimSpace(r.PostFormValue("last_name"))

	if driverName != "" && firstName == "" {
		parts := strings.Fields(driverName)
		if len(parts) > 1 {
			firstName = parts[0]
			lastName = strings.Join(parts[1:], " ")
		} else if len(parts) == 1 {
			firstName = parts[0]
			lastName = "Driver"
		}
	}

	if firstName != "" && driverPhone != "" {
		if lastName == "" {
			lastName = "Driver"
		}
		if driverLicense == "" {
			driverLicense = "DL-" + driverPhone
		}
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()
		createDriverUC := driverapp.NewCreateDriverUseCase(uowImpl, idGenImpl, clockImpl)

		_, _ = createDriverUC.Execute(r.Context(), driverapp.CreateDriverCommand{
			TenantID:        tenantID,
			FirstName:       firstName,
			LastName:        lastName,
			Phone:           driverPhone,
			LicenseNumber:   driverLicense,
			LicenseExpiry:   time.Now().AddDate(5, 0, 0),
			ExperienceYears: 1,
		})
	}

	redirectURL := "/dashboard"
	if vehicleAdded {
		redirectURL = "/tracking"
	}
	http.SetCookie(w, flashCookie("flash_success", "Onboarding completed successfully! Welcome to Avandab."))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
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

	gstNumber := strings.TrimSpace(r.PostFormValue("gst_number"))
	if gstNumber != "" {
		gstNumber = strings.ToUpper(gstNumber)
		if !validate.ValidGSTIN(gstNumber) {
			h.failPage(w, r, fmt.Errorf("invalid GSTIN format: must be 15-character alphanumeric (e.g. 27ABCDE1234F1Z5)"), http.StatusBadRequest, "Invalid GST Number")
			return
		}
	}

	panNumber := strings.TrimSpace(r.PostFormValue("pan_number"))
	if panNumber == "" {
		panNumber = strings.TrimSpace(r.PostFormValue("pan"))
	}
	if panNumber != "" {
		panNumber = strings.ToUpper(panNumber)
		if !validate.ValidPAN(panNumber) {
			h.failPage(w, r, fmt.Errorf("invalid PAN format: must be 10-character alphanumeric (e.g. ABCDE1234F)"), http.StatusBadRequest, "Invalid PAN Number")
			return
		}
	}
	if gstNumber != "" && len(gstNumber) == 15 && panNumber == "" {
		panNumber = gstNumber[2:12]
	}

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
		gstNumber,
		panNumber,
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

// isCompanyIncomplete reports whether a company's mandatory Step 1 compliance profile is incomplete.
func isCompanyIncomplete(c domain.CompanySettings) bool {
	if strings.TrimSpace(c.CompanyName) == "" {
		return true
	}
	if c.Address == nil || strings.TrimSpace(*c.Address) == "" {
		return true
	}
	if c.Phone == nil || strings.TrimSpace(*c.Phone) == "" {
		return true
	}
	if c.Email == nil || strings.TrimSpace(*c.Email) == "" {
		return true
	}
	return false
}
