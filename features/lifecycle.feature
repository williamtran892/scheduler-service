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
  Calendar: this file books MAN01 only in 9 days' time (08:00 to 16:15) and in 10 days' time
  (08:00 to 16:15), in 45-minute windows. Two INTERIM scenarios share a window, one booked by
  Alice and one by Bob: INTERIM runs on Bay 1 or Bay 2, so both always find a pair. A MOT window
  belongs to one scenario, because MOT has one pair (Bay 2, Priya). The rows of an outline whose
  refusals change nothing share one booking, since booking it again returns the same appointment.
    in 9 days:  08:00 malformed requests (Alice), booking key of a cancellation (Bob)
                08:45 check-in (Alice), start (Bob)
                09:30 audit of check-in and start (Alice), dealer start time (Bob)
                10:15 stale version (Alice), stale version changes nothing (Bob)
                11:00 early completion (Alice), cancel a confirmed appointment (Bob)
                11:45 MOT: rebook after an early completion
                12:30 audit of a cancel (Alice), cancel a checked-in appointment (Bob)
                13:15 MOT: rebook after a cancel
                14:00 MOT: rebook after cancelling a checked-in appointment
                14:45 customer cancel (Alice), cancel again (Bob)
                15:30 audit of a repeated cancel (Alice), staff no-show (Bob)
    in 10 days: 08:00 customer-channel refusals (Alice), audit of a no-show (Bob)
                08:45 MOT: rebook after a no-show
                09:30 to 16:15, nine windows: refused transitions (Alice), and that they change
                nothing (Bob)

  Background:
    Given the workshop fixture for dealership "MAN01"

  Scenario: Checking in a confirmed appointment keeps its cells
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 08:45
    When the appointment is checked in
    Then the response is 200
    And the appointment status is "CHECKED_IN" at version 2
    And the appointment owns 3 bay cells from 08:45 to 09:30
    And the appointment owns 3 technician cells from 08:45 to 09:30

  Scenario: Starting a checked-in job keeps its cells and records the actual start
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 08:45
    And the appointment has been checked in
    When the appointment is started
    Then the response is 200
    And the appointment status is "IN_PROGRESS" at version 3
    And the appointment has an actual start time and no actual end time
    And the appointment owns 3 bay cells from 08:45 to 09:30

  Scenario: Checking in and starting a job each append one audit event
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 09:30
    And the appointment has been checked in
    And the appointment has been started
    When the appointment is fetched with its events
    Then the audit trail has exactly one "CHECKED_IN" event through "ADVISOR_CONSOLE"
    And the audit trail has exactly one "STARTED" event through "ADVISOR_CONSOLE"

  Scenario: A start time sent by the dealer system is recorded as the actual start
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 09:30
    And the appointment has been checked in
    When the appointment is started with a start time of 16:00 yesterday
    Then the response is 200
    And the appointment status is "IN_PROGRESS" at version 3
    And the appointment's actual start is 16:00 yesterday

  Scenario: A transition sent with a stale version is refused
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 10:15
    And the appointment has been checked in
    When the appointment is started with version 1
    Then the response is 409 "VERSION_CONFLICT"

  Scenario: A transition refused for a stale version changes nothing
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 10:15
    And the appointment has been checked in
    And an attempt to have the appointment started with version 1 has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CHECKED_IN" at version 2

  Scenario: Completing a job before its booked time frees all its cells
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 11:00
    And the appointment has been checked in
    And the appointment has been started
    When the appointment is completed
    Then the response is 200
    And the appointment status is "COMPLETED" at version 4
    And the appointment has an actual start time and an actual end time
    And the appointment owns no ledger cells

  Scenario: The time of a job completed early can be booked by another customer
    Given "Alice" has booked "MOT" for "YK21 ABC" in 9 days at 11:45
    And the appointment has been checked in
    And the appointment has been started
    And the appointment has been completed
    When "Bob" books "MOT" for "BD70 XYZ" in 9 days at 11:45
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  @design-11.3
  Scenario: Cancelling a confirmed appointment frees its cells
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 11:00
    When the appointment is cancelled
    Then the response is 200
    And the appointment status is "CANCELLED" at version 2
    And the appointment owns no ledger cells

  Scenario: Cancelling an appointment appends one audit event
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 12:30
    And the appointment has been cancelled
    When the appointment is fetched with its events
    Then the audit trail has exactly one "CANCELLED" event through "ADVISOR_CONSOLE"

  @design-11.3
  Scenario: The time of a cancelled appointment can be booked by another customer
    Given "Alice" has booked "MOT" for "YK21 ABC" in 9 days at 13:15
    And the appointment has been cancelled
    When "Bob" books "MOT" for "BD70 XYZ" in 9 days at 13:15
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario: Cancelling a checked-in appointment frees its cells
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 12:30
    And the appointment has been checked in
    When the appointment is cancelled
    Then the response is 200
    And the appointment status is "CANCELLED" at version 3
    And the appointment owns no ledger cells

  Scenario: The time of a cancelled checked-in appointment can be booked by another customer
    Given "Alice" has booked "MOT" for "YK21 ABC" in 9 days at 14:00
    And the appointment has been checked in
    And the appointment has been cancelled
    When "Bob" books "MOT" for "BD70 XYZ" in 9 days at 14:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario: A customer channel cancels a confirmed appointment
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 14:45
    When the appointment is cancelled through "CUSTOMER_WEB"
    Then the response is 200
    And the appointment status is "CANCELLED" at version 2

  Scenario: Cancelling an appointment that is already cancelled returns it unchanged
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 14:45
    And the appointment has been cancelled through "CUSTOMER_WEB"
    When the appointment is cancelled with version 1 through "CUSTOMER_WEB"
    Then the response is 200
    And the appointment status is "CANCELLED" at version 2

  Scenario: Cancelling an appointment twice appends one audit event
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 15:30
    And the appointment has been cancelled through "CUSTOMER_WEB"
    And the appointment has been cancelled with version 1 through "CUSTOMER_WEB"
    When the appointment is fetched with its events
    Then the audit trail has exactly one "CANCELLED" event through "CUSTOMER_WEB"

  @idempotency
  Scenario: A cancelled appointment's booking key cannot be booked again
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 08:00
    And the appointment has been cancelled
    When "Bob" books "INTERIM" for "BD70 XYZ" in 9 days at 08:00
    Then the response is 409 "DUPLICATE_BOOKING"
    And the problem carries the first booking as the existing appointment with service type "INTERIM"

  Scenario: Staff mark a confirmed appointment as a no-show, freeing its cells
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 15:30
    When the appointment is marked a no-show
    Then the response is 200
    And the appointment status is "NO_SHOW" at version 2
    And the appointment owns no ledger cells

  Scenario: Marking a no-show appends one audit event
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 10 days at 08:00
    And the appointment has been marked a no-show
    When the appointment is fetched with its events
    Then the audit trail has exactly one "NO_SHOW" event through "ADVISOR_CONSOLE"

  Scenario: The time of a no-show can be booked by another customer
    Given "Alice" has booked "MOT" for "YK21 ABC" in 10 days at 08:45
    And the appointment has been marked a no-show
    When "Bob" books "MOT" for "BD70 XYZ" in 10 days at 08:45
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario Outline: A transition the appointment's status does not allow is refused
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at <start>
    And the appointment has reached "<status>"
    When the appointment is <transition>
    Then the response is 409 "INVALID_TRANSITION"

    Examples:
      | start | status      | transition       |
      | 09:30 | CONFIRMED   | started          |
      | 10:15 | CONFIRMED   | completed        |
      | 11:00 | CHECKED_IN  | checked in       |
      | 11:45 | CHECKED_IN  | marked a no-show |
      | 12:30 | IN_PROGRESS | cancelled        |
      | 13:15 | COMPLETED   | cancelled        |
      | 14:00 | CANCELLED   | checked in       |
      | 14:45 | NO_SHOW     | started          |
      | 15:30 | NO_SHOW     | cancelled        |

  Scenario Outline: A transition the appointment's status does not allow changes nothing
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 10 days at <start>
    And the appointment has reached "<status>"
    And an attempt to have the appointment <transition> has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "<status>" at version <version>

    Examples:
      | start | status      | version | transition       |
      | 09:30 | CONFIRMED   | 1       | started          |
      | 10:15 | CONFIRMED   | 1       | completed        |
      | 11:00 | CHECKED_IN  | 2       | checked in       |
      | 11:45 | CHECKED_IN  | 2       | marked a no-show |
      | 12:30 | IN_PROGRESS | 3       | cancelled        |
      | 13:15 | COMPLETED   | 4       | cancelled        |
      | 14:00 | CANCELLED   | 2       | checked in       |
      | 14:45 | NO_SHOW     | 2       | started          |
      | 15:30 | NO_SHOW     | 2       | cancelled        |

  Scenario Outline: Day-of transitions are refused to customer channels
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 08:00
    When the appointment is <transition> through "<channel>"
    Then the response is 403 "FORBIDDEN"

    Examples:
      | transition       | channel      |
      | checked in       | CUSTOMER_WEB |
      | started          | CUSTOMER_APP |
      | completed        | CUSTOMER_WEB |
      | marked a no-show | CUSTOMER_APP |

  Scenario Outline: A day-of transition refused to a customer channel changes nothing
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 08:00
    And an attempt to have the appointment <transition> through "<channel>" has been refused
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
