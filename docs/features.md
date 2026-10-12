# Features

This page lists the behaviour the system delivers today. Each entry links to the Gherkin features
that prove it. Update it in the same change that delivers or alters a feature.

| Feature | What it does | Proven by |
|---|---|---|
| Health check | `GET /healthz` reports whether the service can reach its database | `features/health.feature` |
| Book an appointment | `POST /appointments` confirms an appointment for a customer's vehicle on one compatible bay and one qualified technician, claimed in one transaction so a bay or technician is never double-booked; repeating a booking returns the same appointment; refusals are problems with a stable code and, for a taken slot, nearby alternatives. `GET /appointments/{appointmentId}` reads it back with the ledger cells it owns and, on request, its audit trail. The caller states its channel in `X-Channel`, which decides the staff or customer view | `features/booking.feature`, `features/claim_ledger.feature`, `features/idempotency.feature`, `features/concurrency.feature` |
