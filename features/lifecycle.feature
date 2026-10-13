@spec-002 @lifecycle @R3 @design-4.3
Feature: Appointment lifecycle
  After booking, staff move an appointment through the day: check-in, start and complete, or
  cancel and no-show. Every transition is sent with the version the caller last saw, changes the
  ledger as the state machine says, appends an audit event and an outbound event in the same
  step, and returns the updated record. A refused transition changes nothing. (design 4.3, 5.6,
  6, 11.4 "Cancel / no-show", "Complete"; notes 4.4, 5.4; checkInAppointment, startAppointment,
  completeAppointment, cancelAppointment, markNoShow)

  Dates are relative to the day the suite runs and times are Europe/London. The database is not
  reset between scenarios, so each scenario books its own window.
  Calendar: this file books MAN01 only in 9 days' time (08:00 to 16:45) and in 10 days' time
  (08:00 to 16:45).

  Background:
    Given the workshop fixture for dealership "MAN01"

  Scenario: Checking in and starting a job keeps its cells and records the actual start
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 09:00
    When the appointment is checked in
    Then the response is 200
    And the appointment status is "CHECKED_IN" at version 2
    And the appointment owns 3 bay cells from 09:00 to 09:45
    And the appointment owns 3 technician cells from 09:00 to 09:45
    When the appointment is started
    Then the response is 200
    And the appointment status is "IN_PROGRESS" at version 3
    And the appointment has an actual start time and no actual end time
    And the appointment owns 3 bay cells from 09:00 to 09:45
    When the appointment is fetched with its events
    Then the audit trail has exactly one "CHECKED_IN" event through "ADVISOR_CONSOLE"
    And the audit trail has exactly one "STARTED" event through "ADVISOR_CONSOLE"

  Scenario: A start time sent by the dealer system is recorded as the actual start
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 10:00
    And the appointment has been checked in
    When the appointment is started with a start time of 16:00 yesterday
    Then the response is 200
    And the appointment status is "IN_PROGRESS" at version 3
    And the appointment's actual start is 16:00 yesterday

  Scenario: A transition sent with a stale version is refused and changes nothing
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 11:00
    And the appointment has been checked in
    When the appointment is started with version 1
    Then the response is 409 "VERSION_CONFLICT"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CHECKED_IN" at version 2

  Scenario: Completing a job before its booked time frees all its cells
    Given "Alice" has booked "MOT" for "YK21 ABC" in 9 days at 12:00
    And the appointment has been checked in
    And the appointment has been started
    When the appointment is completed
    Then the response is 200
    And the appointment status is "COMPLETED" at version 4
    And the appointment has an actual start time and an actual end time
    And the appointment owns no ledger cells
    When "Bob" books "MOT" for "BD70 XYZ" in 9 days at 12:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  @design-11.3
  Scenario: Cancelling a confirmed appointment frees its cells for another booking
    Given "Alice" has booked "MOT" for "YK21 ABC" in 9 days at 13:00
    When the appointment is cancelled
    Then the response is 200
    And the appointment status is "CANCELLED" at version 2
    And the appointment owns no ledger cells
    When the appointment is fetched with its events
    Then the audit trail has exactly one "CANCELLED" event through "ADVISOR_CONSOLE"
    When "Bob" books "MOT" for "BD70 XYZ" in 9 days at 13:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario: Cancelling a checked-in appointment frees its cells
    Given "Alice" has booked "MOT" for "YK21 ABC" in 9 days at 14:00
    And the appointment has been checked in
    When the appointment is cancelled
    Then the response is 200
    And the appointment status is "CANCELLED" at version 3
    And the appointment owns no ledger cells
    When "Bob" books "MOT" for "BD70 XYZ" in 9 days at 14:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario: Cancelling an appointment that is already cancelled returns it unchanged
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 15:00
    When the appointment is cancelled through "CUSTOMER_WEB"
    Then the response is 200
    And the appointment status is "CANCELLED" at version 2
    When the appointment is cancelled with version 1 through "CUSTOMER_WEB"
    Then the response is 200
    And the appointment status is "CANCELLED" at version 2
    When the appointment is fetched with its events
    Then the audit trail has exactly one "CANCELLED" event through "CUSTOMER_WEB"

  @idempotency
  Scenario: A cancelled appointment's booking key cannot be booked again
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 16:00
    And the appointment has been cancelled
    When "Alice" books "INTERIM" for "YK21 ABC" in 9 days at 16:00
    Then the response is 409 "DUPLICATE_BOOKING"
    And the problem carries the first booking as the existing appointment with service type "INTERIM"

  Scenario: Staff mark a confirmed appointment as a no-show, freeing its cells
    Given "Alice" has booked "MOT" for "YK21 ABC" in 10 days at 09:00
    When the appointment is marked a no-show
    Then the response is 200
    And the appointment status is "NO_SHOW" at version 2
    And the appointment owns no ledger cells
    When the appointment is fetched with its events
    Then the audit trail has exactly one "NO_SHOW" event through "ADVISOR_CONSOLE"
    When "Bob" books "MOT" for "BD70 XYZ" in 10 days at 09:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario Outline: A transition the appointment's status does not allow is refused and changes nothing
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at <start>
    And the appointment has reached "<status>"
    When the appointment is <transition>
    Then the response is 409 "INVALID_TRANSITION"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "<status>" at version <version>

    Examples:
      | start | status      | version | transition       |
      | 10:00 | CONFIRMED   | 1       | started          |
      | 10:45 | CONFIRMED   | 1       | completed        |
      | 11:30 | CHECKED_IN  | 2       | checked in       |
      | 12:15 | CHECKED_IN  | 2       | marked a no-show |
      | 13:00 | IN_PROGRESS | 3       | cancelled        |
      | 13:45 | COMPLETED   | 4       | cancelled        |
      | 14:30 | CANCELLED   | 2       | checked in       |
      | 15:15 | NO_SHOW     | 2       | started          |
      | 16:00 | NO_SHOW     | 2       | cancelled        |

  Scenario Outline: Day-of transitions are refused to customer channels
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 08:00
    When the appointment is <transition> through "<channel>"
    Then the response is 403 "FORBIDDEN"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1

    Examples:
      | transition       | channel      |
      | checked in       | CUSTOMER_WEB |
      | started          | CUSTOMER_APP |
      | completed        | CUSTOMER_WEB |
      | marked a no-show | CUSTOMER_APP |

  Scenario Outline: A malformed transition request is rejected
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 08:00
    And a "<operation>" request is prepared for the appointment
    And the request has <change>
    When the transition request is sent
    Then the response is <status> "<code>" naming the field "<field>"

    Examples:
      | operation  | change                                   | status | code             | field           |
      | cancel     | "expectedVersion" removed                | 400    | VALIDATION_ERROR | expectedVersion |
      | no-show    | "expectedVersion" set to "one"           | 400    | VALIDATION_ERROR | expectedVersion |
      | check-in   | "expectedVersion" set to 0               | 422    | INVALID_VALUE    | expectedVersion |
      | check-in   | "mileage" set to -1                      | 422    | INVALID_VALUE    | mileage         |
      | start      | "startedAt" set to "2026-11-02T12:00:00" | 400    | VALIDATION_ERROR | startedAt       |
      | complete   | no channel                               | 400    | VALIDATION_ERROR | X-Channel       |
      | reschedule | "startAt" removed                        | 400    | VALIDATION_ERROR | startAt         |
      | reschedule | "technicianId" set to "Lee"              | 400    | VALIDATION_ERROR | technicianId    |

  Scenario: A transition on an unknown appointment is not found
    When appointment "00000000-0000-4000-8000-000000000000" is cancelled
    Then the response is 404 "NOT_FOUND"
