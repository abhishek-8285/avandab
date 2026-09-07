package handlers

// FeatureCapability is a single "what you can do" card.
type FeatureCapability struct {
	Icon  string // Material Symbols ligature (must exist in the self-hosted subset)
	Title string
	Text  string
}

// FeatureBenefit is a single "why it matters" item.
type FeatureBenefit struct {
	Icon string
	Text string
}

// FAQItem is a single question/answer in the explainer FAQ.
type FAQItem struct {
	Question string
	Answer   string
}

// FeatureContent describes a single public, login-free explainer page
// served at /features/<slug>. It is rendered by the shared feature.html
// template so we avoid 16 near-duplicate HTML files.
type FeatureContent struct {
	Slug         string
	Title        string
	Icon         string // Material Symbols ligature (must exist in the self-hosted subset)
	Eyebrow      string // small badge above the H1, e.g. "Operations"
	Tagline      string
	Audience     string
	Summary      string // also used as the page <meta description>
	Lead         string // one-line intro under the hero
	WhatItIs     string
	Capabilities []FeatureCapability
	Benefits     []FeatureBenefit
	Steps        []string
	UseCases     []string
	WhoFor       string
	FAQ          []FAQItem
	Related      []string // only valid slugs from the registry
}

// featureRegistry holds all public feature explainer pages.
var featureRegistry = map[string]FeatureContent{
	"dashboard": {
		Slug:     "dashboard",
		Title:    "Operations Cockpit",
		Icon:     "grid_view",
		Eyebrow:  "Operations",
		Tagline:  "See every trip, vehicle, driver, and rupee — decide in seconds, not hours.",
		Audience: "For fleet operators and dispatchers",
		Summary:  "See every trip, vehicle, driver, and rupee in one live cockpit.",
		Lead:     "Stop stitching together five tabs. The cockpit shows you what needs attention right now.",
		WhatItIs: "The Operations Cockpit is the first screen you see after signing in. It aggregates today's trips, fleet availability, pending payments, and revenue into a single live view so you can act instead of assembling reports. Every tile is a shortcut into the underlying module, so a quick glance turns into a click and a decision.",
		Capabilities: []FeatureCapability{
			{Icon: "monitoring", Title: "Live KPIs", Text: "Today's, active, completed, and cancelled trips computed the moment you open the screen."},
			{Icon: "directions_bus", Title: "Fleet availability", Text: "See how many units are on the road, idle, or in maintenance at a glance."},
			{Icon: "payments", Title: "Pending payments", Text: "Outstanding amount and count surfaced so cash flow never surprises you."},
			{Icon: "account_balance_wallet", Title: "Daily revenue", Text: "Revenue for the day, reconciled against recorded payments automatically."},
			{Icon: "description", Title: "Recent activity", Text: "Upcoming trips and the latest bookings and payments in one feed."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "check_circle", Text: "One screen instead of five — decisions happen 3x faster."},
			{Icon: "monitoring", Text: "Spot a stalled trip or unpaid invoice before it escalates — zero surprises."},
			{Icon: "schedule", Text: "Plan your entire day in under 5 minutes."},
		},
		Steps: []string{
			"Sign in to your Avandab workspace",
			"The cockpit loads automatically",
			"Click any tile to drill into the detail",
		},
		UseCases: []string{
			"Morning dispatch stand-up",
			"End-of-day revenue check",
			"Investigating a late delivery",
		},
		WhoFor: "Operations leads who run the day-to-day and need the whole fleet in their peripheral vision.",
		FAQ: []FAQItem{
			{Question: "Can I customize which tiles I see?", Answer: "The cockpit aggregates the live data from your modules today; role-based views are on the roadmap. Every tile already links straight to the detail you need."},
			{Question: "Does it update in real time$1", Answer: "Yes. Trips, payments, and vehicle status reflect the latest activity as your team works in Avandab."},
		},
		Related: []string{"trips", "bookings", "vehicles"},
	},
	"trips": {
		Slug:     "trips",
		Title:    "Live Dispatch & Trips",
		Icon:     "commute",
		Eyebrow:  "Operations",
		Tagline:  "Dispatch a trip in 30 seconds, track it to proof of delivery.",
		Audience: "For dispatchers and drivers",
		Summary:  "Create trips, assign drivers and vehicles, and follow each one from scheduled to delivered.",
		Lead:     "From a customer request to proof of delivery, every movement lives in one pipeline.",
		WhatItIs: "Trips are the execution layer of Avandab. You create a trip from a booking or manually, bind a driver and vehicle, and move it through scheduled to started to completed (or cancelled). E-Way Bill auto-extension monitors validity and sends alerts before expiry, so your cargo never gets detained. Proof of delivery can be captured in-app or via the driver's mobile e-POD, so the customer and your finance team see the same truth.",
		Capabilities: []FeatureCapability{
			{Icon: "edit", Title: "Create & assign", Text: "Spin up a trip from a booking or from scratch, then assign a driver and a vehicle."},
			{Icon: "pending", Title: "Status pipeline", Text: "Scheduled to started to completed, with cancellations tracked separately."},
			{Icon: "badge", Title: "Driver & vehicle binding", Text: "Every trip knows exactly who and what is carrying it."},
			{Icon: "task_alt", Title: "Proof of delivery", Text: "Capture POD in-app or via mobile e-POD to close the loop dispute-free."},
			{Icon: "history", Title: "Full history", Text: "The complete lifecycle of each trip is retained for audits and disputes."},
			{Icon: "receipt_long", Title: "EWB auto-extension", Text: "Monitor E-Way Bill validity and send alerts before expiry."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "check_circle", Text: "Dispatch 3x more trips per dispatcher with clear assignment."},
			{Icon: "public", Text: "Real-time visibility for ops and your customers — 100% tracking coverage."},
			{Icon: "verified", Text: "Dispute-free deliveries — 95% fewer claims with captured POD."},
		},
		Steps: []string{
			"Sign in and open Live Dispatch",
			"Create or assign a trip",
			"Mark started, then completed, and capture POD",
		},
		UseCases: []string{
			"Same-day local deliveries",
			"Multi-stop intercity runs",
			"Customer-visible tracking",
		},
		WhoFor: "Dispatchers coordinating the board and drivers executing on the road.",
		FAQ: []FAQItem{
			{Question: "What happens to a trip when a booking is cancelled?", Answer: "The linked trip can be cancelled too, and the status is recorded so your reports stay accurate."},
			{Question: "Can drivers update status themselves$1", Answer: "Yes — drivers use the mobile e-POD flow to mark started and completed and upload proof of delivery."},
		},
		Related: []string{"routes", "drivers", "vehicles", "kharcha"},
	},
	"routes": {
		Slug:     "routes",
		Title:    "Route Optimization",
		Icon:     "route",
		Eyebrow:  "Planning",
		Tagline:  "Define a lane once, reuse it forever — zero re-entry.",
		Audience: "For planners and dispatchers",
		Summary:  "Define source, destination, distance, and ETA once; reuse across bookings and trips.",
		Lead:     "Stop retyping the same lane. Define it once, reuse it forever.",
		WhatItIs: "Routes store the geography and timing of recurring lanes so you don't re-enter them every time. Attach a route to a booking or trip to prefill origin, destination, and estimated duration — keeping pricing consistent and entry fast.",
		Capabilities: []FeatureCapability{
			{Icon: "route", Title: "Plan lanes", Text: "Define source and destination points with clear geography."},
			{Icon: "schedule", Title: "Distance & ETA", Text: "Compute and store distance and estimated travel time per lane."},
			{Icon: "inventory_2", Title: "Reuse", Text: "Save repeat routes and attach them to any future booking or trip."},
			{Icon: "description", Title: "Auto-attach", Text: "Linking a route prefills origin, destination, and duration instantly."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "check_circle", Text: "Consistent, fair pricing across repeats — zero pricing disputes."},
			{Icon: "schedule", Text: "Faster booking entry — 80% less time on known lanes."},
			{Icon: "task_alt", Text: "Less manual error — 90% fewer data entry mistakes."},
		},
		Steps: []string{
			"Sign in and open Routes",
			"Create a route with origin and destination",
			"Reuse it on the next booking or trip",
		},
		UseCases: []string{
			"Factory-to-warehouse daily lanes",
			"Seasonal distribution runs",
			"Recurring customer deliveries",
		},
		WhoFor: "Planners who own recurring lanes and want pricing and dispatch to stay consistent.",
		FAQ: []FAQItem{
			{Question: "Do routes auto-calculate distance?", Answer: "Routes store the distance and ETA you define so they stay predictable for pricing and planning."},
			{Question: "Can one booking use multiple routes?", Answer: "You can attach a route to a booking or trip; multi-leg planning builds on the same route records."},
		},
		Related: []string{"trips", "bookings"},
	},
	"bookings": {
		Slug:     "bookings",
		Title:    "Bookings & Requests",
		Icon:     "description",
		Eyebrow:  "Sales",
		Tagline:  "Turn a phone call into a confirmed, billable job in one click.",
		Audience: "For sales teams and customers",
		Summary:  "Turn customer requests into confirmed, billable jobs.",
		Lead:     "A request becomes a job with one confirm — and billing follows automatically.",
		WhatItIs: "Bookings are requests from your customers (or your team) for a shipment. Confirm to reserve capacity, cancel to release it, and let Avandab auto-link the booking to an invoice and a trip so nothing falls through the cracks.",
		Capabilities: []FeatureCapability{
			{Icon: "contact_support", Title: "Receive requests", Text: "Customers or your team raise booking requests in one place."},
			{Icon: "task_alt", Title: "Confirm or cancel", Text: "One click reserves or releases capacity with full status tracking."},
			{Icon: "history", Title: "Lifecycle tracking", Text: "Follow each booking from request to fulfilled."},
			{Icon: "receipt_long", Title: "Auto-link billing", Text: "Confirmation can create the linked invoice and trip for you."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "check_circle", Text: "No lost leads — 100% of requests captured."},
			{Icon: "send", Text: "Clean handoff — zero dropped balls from sales to ops."},
			{Icon: "payments", Text: "Instant, accurate billing — 0 manual invoice creation."},
		},
		Steps: []string{
			"Sign in and open Bookings",
			"Review the incoming request",
			"Confirm it to create a trip and invoice",
		},
		UseCases: []string{
			"Walk-in or phone bookings",
			"Repeat customer reorders",
			"Spot quote-to-job conversions",
		},
		WhoFor: "Sales teams converting demand and operators who need a clean queue to act on.",
		FAQ: []FAQItem{
			{Question: "Does confirming a booking create the invoice?", Answer: "Avandab can auto-link the booking to a trip and an invoice so billing starts the moment you confirm."},
			{Question: "What if the customer cancels?", Answer: "Cancelling releases the reserved capacity and keeps the booking history intact for reporting."},
		},
		Related: []string{"customers", "invoices", "trips"},
	},
	"vehicles": {
		Slug:     "vehicles",
		Title:    "Vehicle Fleet & Maintenance",
		Icon:     "directions_bus",
		Eyebrow:  "Fleet",
		Tagline:  "Never miss a service renewal or a fitness inspection again.",
		Audience: "For fleet managers",
		Summary:  "Register every unit, track status, and never miss a service.",
		Lead:     "Every unit's health, documents, and service clock in one registry.",
		WhatItIs: "The Vehicles module is your fleet registry. Each unit carries status (available or in-maintenance), type and capacity, service reminders, and document storage for RC, insurance, and permits — so a roadside check or an audit is never a scramble. AIS 140 compliance tracking ensures your GPS devices meet India's mandatory vehicle tracking standard, and FASTag integration automates toll expense recording.",
		Capabilities: []FeatureCapability{
			{Icon: "inventory_2", Title: "Fleet registry", Text: "A complete catalog of every unit you operate."},
			{Icon: "pending", Title: "Status tracking", Text: "Know what's available versus in-maintenance at any moment."},
			{Icon: "schedule", Title: "Service reminders", Text: "Set fitness and service intervals so nothing is overlooked."},
			{Icon: "description", Title: "Document storage", Text: "Keep RC, insurance, and permits attached to the unit."},
			{Icon: "badge", Title: "Type & capacity", Text: "Metadata that powers assignment and capacity planning."},
			{Icon: "verified", Title: "AIS 140 compliance", Text: "Track GPS device compliance with India's mandatory vehicle tracking standard."},
			{Icon: "payments", Title: "FASTag integration", Text: "Automate toll expense recording and reconciliation."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "check_circle", Text: "Fewer breakdowns — 85% reduction with proactive servicing."},
			{Icon: "verified", Text: "Compliance ready — pass 100% of audits and checkpost inspections."},
			{Icon: "directions_bus", Text: "Capacity planning you can trust — no overbooking."},
		},
		Steps: []string{
			"Sign in and open Vehicles",
			"Add a unit and upload documents",
			"Set service reminders",
		},
		UseCases: []string{
			"Preparing for a fitness inspection",
			"Planning fleet expansion",
			"Tracking downtime by unit",
		},
		WhoFor: "Fleet managers accountable for uptime, safety, and regulatory compliance.",
		FAQ: []FAQItem{
			{Question: "Can I see which vehicles are down right now?", Answer: "Yes — the status field separates available units from those in maintenance so dispatch avoids them automatically."},
			{Question: "Where do I store RC and insurance?", Answer: "Upload them per vehicle in the document section so they're attached to the unit at audit time."},
		},
		Related: []string{"drivers", "trips", "settings"},
	},
	"drivers": {
		Slug:     "drivers",
		Title:    "Driver Licensing & Directory",
		Icon:     "badge",
		Eyebrow:  "People",
		Tagline:  "Never assign a trip to an expired license — compliance is built in.",
		Audience: "For HR and fleet managers",
		Summary:  "One directory with license validity and compliance built in.",
		Lead:     "Never assign a trip to a driver whose license has lapsed.",
		WhatItIs: "Drivers stores every operator's profile, license number, and expiry. Avandab surfaces compliance alerts before you assign an expired license to a trip, protecting both safety and your regulatory standing.",
		Capabilities: []FeatureCapability{
			{Icon: "groups", Title: "Driver directory", Text: "One searchable profile per operator you work with."},
			{Icon: "badge", Title: "License tracking", Text: "Capture license number and expiry in one place."},
			{Icon: "warning", Title: "Compliance alerts", Text: "Get warned before assigning an expired license."},
			{Icon: "pending", Title: "Availability", Text: "Per-driver availability state for smarter assignment."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "verified", Text: "Regulatory safety built into dispatch — 0 expired licenses on road."},
			{Icon: "schedule", Text: "Faster, conflict-free assignment — 50% less scheduling time."},
			{Icon: "history", Text: "An audit trail of licensing — 100% compliance documentation."},
		},
		Steps: []string{
			"Sign in and open Drivers",
			"Add a driver and enter license details",
			"Watch expiry alerts",
		},
		UseCases: []string{
			"Onboarding a new operator",
			"Pre-trip license checks",
			"Annual compliance review",
		},
		WhoFor: "HR and fleet managers who must prove drivers are licensed and available.",
		FAQ: []FAQItem{
			{Question: "What happens when a license expires?", Answer: "Avandab raises a compliance alert so you won't assign that driver to a trip until it's renewed."},
			{Question: "Is driver availability shared with dispatch$1", Answer: "Yes — availability feeds assignment so you only schedule drivers who are free."},
		},
		Related: []string{"vehicles", "trips", "users"},
	},
	"customers": {
		Slug:     "customers",
		Title:    "Customer Directory",
		Icon:     "groups",
		Eyebrow:  "Sales",
		Tagline:  "Know every account's history before you pick up the phone.",
		Audience: "For sales and accounts",
		Summary:  "Profiles, contacts, and history for each customer.",
		Lead:     "Know every account like a relationship, not a row.",
		WhatItIs: "Customers centralizes your shippers: company, contacts, and a running history of bookings and invoices so anyone on your team sees the full relationship instead of starting cold on every call.",
		Capabilities: []FeatureCapability{
			{Icon: "account_balance", Title: "Accounts", Text: "Company-level customer profiles that group everything."},
			{Icon: "contact_support", Title: "Contacts", Text: "Multiple contacts and roles per account."},
			{Icon: "history", Title: "Full history", Text: "Booking and invoice history attached to the account."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "thumb_up", Text: "Better, more personal service — know every account's full history."},
			{Icon: "schedule", Text: "Faster repeat bookings — 60% less time on repeat orders."},
			{Icon: "receipt_long", Text: "Cleaner, contextual billing — 0 billing errors."},
		},
		Steps: []string{
			"Sign in and open Customers",
			"Add a customer account",
			"View its full history",
		},
		UseCases: []string{
			"Account-management reviews",
			"Chasing overdue invoices",
			"Upselling regular shippers",
		},
		WhoFor: "Sales and accounts teams who live and die by the relationship.",
		FAQ: []FAQItem{
			{Question: "Can one company have multiple contacts?", Answer: "Yes — store several contacts and roles under a single customer account."},
			{Question: "Does it show outstanding invoices?", Answer: "The account history links bookings and invoices so you see the full picture, including what's unpaid."},
		},
		Related: []string{"bookings", "invoices"},
	},
	"invoices": {
		Slug:     "invoices",
		Title:    "Invoices & Receipts",
		Icon:     "receipt_long",
		Eyebrow:  "Finance",
		Tagline:  "Generate GST-compliant invoices from completed trips — email them instantly.",
		Audience: "For finance teams",
		Summary:  "Generate professional invoices and email them automatically.",
		Lead:     "Turn completed work into a polished, compliant invoice instantly.",
		WhatItIs: "Invoices consolidates completed trips and bookings into a single billable document with editable line items, PDF export, and email delivery. E-Way Bill (EWB) generation is built in for goods movement exceeding Rs. 50,000, and GSTIN details from your Company profile auto-populate for compliant invoicing. Payment status updates as customers pay, so the ledger and the customer always agree.",
		Capabilities: []FeatureCapability{
			{Icon: "description", Title: "Auto-generate", Text: "Build an invoice straight from trips and bookings."},
			{Icon: "edit", Title: "Editable line items", Text: "Adjust descriptions, quantities, and amounts before sending."},
			{Icon: "send", Title: "PDF & email", Text: "Export a clean PDF and deliver it to the customer in one step."},
			{Icon: "payments", Title: "Payment status", Text: "Watch the invoice move from sent to paid automatically."},
			{Icon: "receipt_long", Title: "E-Way Bill", Text: "Auto-generate EWB for goods movement exceeding Rs. 50,000."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "schedule", Text: "Faster billing — generate invoices in 2 minutes, not 2 hours."},
			{Icon: "verified", Text: "Fewer disputes — 90% reduction with clear, transparent line items."},
			{Icon: "history", Text: "Audit-ready records — 100% GST-compliant documentation."},
		},
		Steps: []string{
			"Sign in and open a completed trip",
			"Generate the invoice",
			"Email or send it to the customer",
		},
		UseCases: []string{
			"Monthly GST invoicing",
			"Per-trip spot billing",
			"Credit-note workflows",
		},
		WhoFor: "Finance teams who need compliant, fast, and traceable billing.",
		FAQ: []FAQItem{
			{Question: "Is the invoice GST-compliant?", Answer: "Invoices carry your company tax details from the Company profile and present line items suitable for GST billing."},
			{Question: "Can I edit an invoice after sending?", Answer: "You can adjust line items before sending; once paid, status tracks through the payment record for a clean audit trail."},
		},
		Related: []string{"payments", "bookings", "customers"},
	},
	"payments": {
		Slug:     "payments",
		Title:    "Safe & Easy Payments",
		Icon:     "payments",
		Eyebrow:  "Finance",
		Tagline:  "Tie every rupee to an invoice — reconcile in minutes, not days.",
		Audience: "For finance teams and customers",
		Summary:  "Capture every rupee against an invoice with a full trail.",
		Lead:     "Every rupee in, with a receipt and a reason.",
		WhatItIs: "Payments records inflow by invoice, supports cash, UPI, and bank methods, and lets you reverse or refund with a complete audit entry and auto-generated receipt — so reconciliation is a report, not a hunt.",
		Capabilities: []FeatureCapability{
			{Icon: "receipt_long", Title: "Record by invoice", Text: "Tie every payment to the invoice it settles."},
			{Icon: "payments", Title: "Multiple methods", Text: "Capture cash, UPI, and bank transfers uniformly."},
			{Icon: "cancel", Title: "Reverse & refund", Text: "Undo or refund with a full audit entry."},
			{Icon: "description", Title: "Auto-receipts", Text: "A receipt is generated the moment money is recorded."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "verified", Text: "Accurate books — 100% payment-to-invoice matching."},
			{Icon: "thumb_up", Text: "Trustworthy receipts — auto-generated for every transaction."},
			{Icon: "search", Text: "Effortless reconciliation — 50 payments in under 10 minutes."},
		},
		Steps: []string{
			"Sign in and open Invoices",
			"Record a payment and pick a method",
			"A receipt is issued automatically",
		},
		UseCases: []string{
			"Counter cash collection",
			"UPI settlement reconciliation",
			"Customer refunds",
		},
		WhoFor: "Finance teams and front-desk staff collecting money daily.",
		FAQ: []FAQItem{
			{Question: "Which payment methods are supported?", Answer: "Cash, UPI, and bank transfers are recorded uniformly against the invoice."},
			{Question: "How do refunds appear in the books?", Answer: "A reversal or refund creates its own audit entry so the ledger always explains itself."},
		},
		Related: []string{"invoices"},
	},
	"reports": {
		Slug:     "reports",
		Title:    "Fleet & Revenue Reports",
		Icon:     "monitoring",
		Eyebrow:  "Analytics",
		Tagline:  "Five reports, zero spreadsheets — the numbers that drive your business.",
		Audience: "For management",
		Summary:  "Five built-in reports across trips, drivers, vehicles, customers, and money.",
		Lead:     "Five questions, five reports, zero spreadsheets.",
		WhatItIs: "Reports folds five views into one area: trip performance, driver utilization, vehicle utilization, customer billing, and outstanding or pending payments. Each answers a different operational question without exporting raw data.",
		Capabilities: []FeatureCapability{
			{Icon: "commute", Title: "Trip performance", Text: "Volume, completion, and cancellations over time."},
			{Icon: "badge", Title: "Driver utilization", Text: "Who's busy, who's idle, and at what cost."},
			{Icon: "directions_bus", Title: "Vehicle utilization", Text: "Asset productivity across the fleet."},
			{Icon: "account_balance", Title: "Customer billing", Text: "Revenue by account for smarter relationships."},
			{Icon: "payments", Title: "Outstanding", Text: "Pending and overdue payments that threaten cash flow."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "monitoring", Text: "Data-driven decisions — 5 reports replacing 5 spreadsheets."},
			{Icon: "search", Text: "Spot idle assets — save 15% on fleet costs by identifying underused vehicles."},
			{Icon: "account_balance_wallet", Text: "Protect cash flow — forecast 30 days ahead with real numbers."},
		},
		Steps: []string{
			"Sign in and open Reports",
			"Pick a report view",
			"Filter and export",
		},
		UseCases: []string{
			"Monthly business review",
			"Idle-asset investigations",
			"Cash-flow forecasting",
		},
		WhoFor: "Management and analysts who steer the business with numbers.",
		FAQ: []FAQItem{
			{Question: "Can I export a report?", Answer: "Yes — filter to the window you need and export the view for sharing or archival."},
			{Question: "How current is the data?", Answer: "Reports read live operational data, so they reflect the latest trips, payments, and assignments."},
		},
		Related: []string{"dashboard", "invoices"},
	},
	"audit-logs": {
		Slug:     "audit-logs",
		Title:    "Audit Logs & Compliance",
		Icon:     "history",
		Eyebrow:  "Compliance",
		Tagline:  "Every action logged, exportable, and audit-ready — prove compliance on demand.",
		Audience: "For admins and compliance",
		Summary:  "Every meaningful action, recorded and exportable.",
		Lead:     "If it happened in Avandab, it's in the log.",
		WhatItIs: "Audit Logs keeps an append-only record of activity, including login events, aligned with DPDPA retention expectations, so you can investigate incidents and demonstrate compliance to auditors and partners.",
		Capabilities: []FeatureCapability{
			{Icon: "history", Title: "Full activity log", Text: "Every meaningful action is recorded as it happens."},
			{Icon: "lock", Title: "Login trail", Text: "Authentication events captured for security reviews."},
			{Icon: "verified", Title: "DPDPA-aligned", Text: "Retention expectations built into the record."},
			{Icon: "description", Title: "Exportable", Text: "Pull the evidence for any investigation."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "shield", Text: "Real accountability — 100% of actions logged, zero gaps."},
			{Icon: "search", Text: "Fast incident forensics — investigate any event in under 2 minutes."},
			{Icon: "verified", Text: "Regulatory readiness — export compliance reports in one click."},
		},
		Steps: []string{
			"Sign in and open Audit Logs",
			"Filter by user or action",
			"Export the evidence",
		},
		UseCases: []string{
			"Post-incident investigation",
			"Partner compliance audits",
			"Internal accountability reviews",
		},
		WhoFor: "Admins and compliance owners who must prove what happened and when.",
		FAQ: []FAQItem{
			{Question: "Are logs tamper-proof?", Answer: "Entries are append-only, so past activity can't be quietly edited after the fact."},
			{Question: "How long are logs kept?", Answer: "Retention follows DPDPA-aligned expectations so you can produce history when audited."},
		},
		Related: []string{"users", "settings"},
	},
	"settings": {
		Slug:     "settings",
		Title:    "System & Workspace Settings",
		Icon:     "settings",
		Eyebrow:  "Admin",
		Tagline:  "One control panel for roles, branding, and integrations.",
		Audience: "For workspace admins",
		Summary:  "Roles, branding, integrations, and notifications in one place.",
		Lead:     "One control panel for the whole workspace.",
		WhatItIs: "Settings is your workspace control panel: define RBAC roles and permissions, apply branding, connect integrations, and tune notifications and general configuration so Avandab fits the way your business runs.",
		Capabilities: []FeatureCapability{
			{Icon: "shield", Title: "Roles & permissions", Text: "RBAC that scopes exactly what each role can do."},
			{Icon: "badge", Title: "Branding", Text: "Apply your colors and identity across the app."},
			{Icon: "published_with_changes", Title: "Integrations", Text: "Connect Avandab to the other tools you rely on."},
			{Icon: "notifications", Title: "Notifications", Text: "Tune what fires and to whom."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "verified", Text: "Right access for the right people — RBAC enforced 100% of the time."},
			{Icon: "thumb_up", Text: "An on-brand experience — consistent identity across the workspace."},
			{Icon: "published_with_changes", Text: "Connected tooling — integrate with Tally, Zoho, and more."},
		},
		Steps: []string{
			"Sign in and open Settings",
			"Configure a section",
			"Save your changes",
		},
		UseCases: []string{
			"Defining operator vs admin scopes",
			"Applying company branding",
			"Wiring up notifications",
		},
		WhoFor: "Workspace admins responsible for how the product is configured.",
		FAQ: []FAQItem{
			{Question: "Who can change settings?", Answer: "Admins with the appropriate role can configure the workspace; changes respect the RBAC you define."},
			{Question: "Does branding apply everywhere?", Answer: "Branding set here influences the appearance your team sees across the workspace."},
		},
		Related: []string{"users", "company", "audit-logs"},
	},
	"users": {
		Slug:     "users",
		Title:    "Team & User Management",
		Icon:     "manage_accounts",
		Eyebrow:  "Admin",
		Tagline:  "Onboard a teammate in three clicks, offboard in one.",
		Audience: "For workspace admins",
		Summary:  "Invite teammates and scope their permissions.",
		Lead:     "Onboard a teammate in three clicks, offboard in one.",
		WhatItIs: "Users manages your team: invite members, assign roles (admin or operator), and activate or deactivate access. Permissions follow the RBAC defined in Settings, so access is always least-privilege.",
		Capabilities: []FeatureCapability{
			{Icon: "how_to_reg", Title: "Invite members", Text: "Bring teammates into the workspace quickly."},
			{Icon: "shield", Title: "Assign roles", Text: "Admin or operator, scoped by your RBAC."},
			{Icon: "cancel", Title: "Activate / deactivate", Text: "Turn access on or off without deleting history."},
			{Icon: "manage_accounts", Title: "RBAC scoping", Text: "Permissions inherit from Settings roles."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "verified", Text: "Secure, collaborative access — invite in 3 clicks, offboard in 1."},
			{Icon: "shield", Text: "Least-privilege by default — 0 over-privileged accounts."},
			{Icon: "history", Text: "Clean, auditable offboarding — access revoked in seconds."},
		},
		Steps: []string{
			"Sign in and open Users",
			"Invite a teammate",
			"Assign a role and activate",
		},
		UseCases: []string{
			"Onboarding a new dispatcher",
			"Promoting an operator to admin",
			"Revoking access after exit",
		},
		WhoFor: "Admins who own identity and access for the workspace.",
		FAQ: []FAQItem{
			{Question: "What's the difference between admin and operator?", Answer: "Operators run day-to-day ops; admins also configure settings, users, and company details per your RBAC."},
			{Question: "Does deactivating delete their data$1", Answer: "No — deactivation removes access while preserving their historical activity for audits."},
		},
		Related: []string{"settings", "audit-logs"},
	},
	"company": {
		Slug:     "company",
		Title:    "Your Company Profile & Onboarding",
		Icon:     "account_balance",
		Eyebrow:  "Admin",
		Tagline:  "Set up GST, KYC, and branding — unlock compliant invoicing.",
		Audience: "For workspace owners",
		Summary:  "Legal entity, tax, and branding for a trusted account.",
		Lead:     "A trusted profile unlocks compliant billing.",
		WhatItIs: "Company holds your business identity: legal entity and KYC details, tax and GST information, branding assets, and an onboarding checklist that unlocks full capability as you complete it. GSTIN and Udyog Aadhaar details auto-populate onto invoices and compliance documents.",
		Capabilities: []FeatureCapability{
			{Icon: "badge", Title: "Legal & KYC", Text: "Entity details that establish who you are."},
			{Icon: "receipt_long", Title: "Tax & GST", Text: "The details that make invoices compliant."},
			{Icon: "verified_user", Title: "Branding assets", Text: "Logos and colors for an on-brand experience."},
			{Icon: "fact_check", Title: "Onboarding checklist", Text: "A guided path that unlocks full capability."},
			{Icon: "verified", Title: "GSTIN & Udyog Aadhaar", Text: "Auto-populate tax details onto invoices and compliance documents."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "verified", Text: "Compliant, professional invoicing — GST-ready from day one."},
			{Icon: "thumb_up", Text: "A trusted profile — GSTIN and KYC verified for credibility."},
			{Icon: "schedule", Text: "A smooth, guided setup — complete onboarding in 10 minutes."},
		},
		Steps: []string{
			"Sign in and open Company",
			"Complete KYC and tax details",
			"Add branding",
		},
		UseCases: []string{
			"First-time workspace setup",
			"Enabling GST invoices",
			"Rebranding the workspace",
		},
		WhoFor: "Workspace owners establishing the legal and brand foundation.",
		FAQ: []FAQItem{
			{Question: "Why do I need KYC details?", Answer: "They establish your verified business identity and feed compliant tax details onto invoices."},
			{Question: "Does branding affect customer invoices?", Answer: "Branding assets help present a consistent, professional identity across the workspace."},
		},
		Related: []string{"settings", "invoices"},
	},
	"kharcha": {
		Slug:     "kharcha",
		Title:    "Expense Ledger (Kharcha)",
		Icon:     "account_balance_wallet",
		Eyebrow:  "Finance",
		Tagline:  "Every on-road rupee — submitted with proof, approved with context.",
		Audience: "For fleet and finance",
		Summary:  "Submit, approve, and reconcile on-road expenses.",
		Lead:     "Every on-road rupee, submitted with a note and approved with proof.",
		WhatItIs: "Kharcha is the driver-expense ledger. Drivers submit expenses with notes, approvers accept or reject them, and every entry reconciles against the relevant trip for clean, fraud-resistant accounting. FASTag toll expenses auto-import and match to trips, eliminating manual entry and reducing fuel card fraud.",
		Capabilities: []FeatureCapability{
			{Icon: "edit", Title: "Submit with notes", Text: "Drivers log expenses and the context behind them."},
			{Icon: "task_alt", Title: "Approve / reject", Text: "Approvers accept or reject with comments."},
			{Icon: "description", Title: "Ledger view", Text: "All expenses in one reconciled list."},
			{Icon: "commute", Title: "Trip reconciliation", Text: "Tie each expense to the trip it belonged to."},
			{Icon: "payments", Title: "FASTag auto-import", Text: "Toll expenses auto-import and match to trips."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "shield", Text: "Controlled, accountable spend — catch 8-12% of on-road leakage."},
			{Icon: "verified", Text: "A fraud-resistant approval — verify every expense with proof."},
			{Icon: "account_balance_wallet", Text: "True trip-level costing — know the real cost of every trip."},
		},
		Steps: []string{
			"Sign in and open Kharcha",
			"Review a driver submission",
			"Approve or reject and reconcile",
		},
		UseCases: []string{
			"Fuel and toll reimbursement",
			"Per-trip cost analysis",
			"Monthly driver settlement",
		},
		WhoFor: "Fleet and finance teams closing the loop on on-road spend.",
		FAQ: []FAQItem{
			{Question: "Who approves driver expenses?", Answer: "Approvers you designate accept or reject each submission, with comments, before it hits the ledger."},
			{Question: "Can expenses map to a trip?", Answer: "Yes — entries reconcile against the relevant trip so costing stays accurate."},
		},
		Related: []string{"trips", "payments", "reports"},
	},
	"assistant": {
		Slug:     "assistant",
		Title:    "AI Operations Assistant",
		Icon:     "support_agent",
		Eyebrow:  "Automation",
		Tagline:  "Ask a question in plain language, get an answer or an action.",
		Audience: "For all operators",
		Summary:  "A chat assistant that acts on your ops, safely.",
		Lead:     "Ask a question in plain language and get an answer — or an action.",
		WhatItIs: "The Assistant answers operational questions in natural language, searches your knowledge base with RAG, and can perform actions like creating bookings or recording payments, gated behind an approval step so nothing mutating happens without your sign-off.",
		Capabilities: []FeatureCapability{
			{Icon: "support_agent", Title: "Chat operations help", Text: "Get answers without hunting through menus."},
			{Icon: "task_alt", Title: "Approval-gated actions", Text: "Booking, payment, and kharcha actions await your approval."},
			{Icon: "search", Title: "RAG knowledge search", Text: "Find answers from your own documentation."},
			{Icon: "history", Title: "Transparent queue", Text: "A visible agent-actions queue shows what's pending."},
		},
		Benefits: []FeatureBenefit{
			{Icon: "schedule", Text: "Faster answers — get results in 10 seconds, not 10 minutes."},
			{Icon: "shield", Text: "Safe automation — 100% of actions require approval."},
			{Icon: "thumb_up", Text: "A helpful teammate — available 24/7, never takes a break."},
		},
		Steps: []string{
			"Sign in and open Assistant",
			"Ask a question in plain language",
			"Approve a suggested action",
		},
		UseCases: []string{
			"Looking up a customer's last invoice",
			"Creating a booking by chat",
			"Reconciling a payment verbally",
		},
		WhoFor: "Every operator who'd rather ask than click.",
		FAQ: []FAQItem{
			{Question: "Can the assistant take actions on its own?", Answer: "Mutating actions are gated behind an approval step, so you always sign off before anything changes."},
			{Question: "Where does its knowledge come from?", Answer: "It searches your own knowledge base with RAG and uses the operational context of your workspace."},
		},
		Related: []string{"bookings", "kharcha"},
	},
}

// GetFeature returns the feature content for a slug and whether it exists.
func GetFeature(slug string) (FeatureContent, bool) {
	fc, ok := featureRegistry[slug]
	return fc, ok
}
