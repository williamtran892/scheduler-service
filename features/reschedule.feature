@spec-002 @reschedule @R2 @R3 @design-5.4
Feature: Reschedule and reassign an appointment
  Staff move a confirmed appointment to a new start, give it another bay or technician, or
  both; a customer may move it in time only. The new cells are claimed with the appointment's
  own cells counting as free, then every cell outside the new runs is released, in one step: if
  the claim fails nothing changes and the customer keeps the original booking. The rules of a
  booking apply to the new start. (design 4.3, 5.4, 7, 11.3, 11.4 "Reschedule and reassign";
  notes 4.4, 4.5; rescheduleAppointment)

  Dates are relative to the day the suite runs and times are Europe/London. The database is not
  reset between scenarios, so each scenario books its own window.
  Calendar: this file books MAN01 only in 8 days' time (15:00 to 17:00), in 11 days' time (08:00
  to 16:45) and in 12 days' time (08:00 to 17:00). A window runs from a scenario's first claimed
  cell to its last, old and new place included. Two INTERIM scenarios share a window, one booked
  by Alice and one by Bob: INTERIM runs on Bay 1 or Bay 2, so both always find a pair. A MOT,
  EV_CHECK or reassignment window belongs to one scenario. The rows of the outline share one
  booking, since its refusals change nothing and booking it again returns the same appointment.
    in 8 days:  15:00 to 16:30 MOT move to a free time
                16:30 to 17:00 EV_CHECK reassignment to an unqualified technician
    in 11 days: 08:00 to 09:30 MOT rebook of the time a move left
                09:30 to 10:45 MOT move that overlaps its own cells
                10:45 to 12:15 MOT rebook of the cells an overlapping move left
                12:15 to 13:45 MOT move to a taken time
                13:45 to 15:15 MOT move to a taken time changes nothing
                15:15 to 16:00 technician reassignment
                16:00 to 16:45 MOT rebook of the technician a reassignment left
    in 12 days: 08:00 to 08:45 customer reassignment (Alice), and that it changes nothing (Bob)
                08:45 to 09:15 EV_CHECK unqualified technician changes nothing
                09:15 to 10:00 MOT bay of the wrong type
                10:00 to 10:45 MOT bay of the wrong type changes nothing
                10:45 to 12:15 moves the booking rules refuse (Alice), audit of a customer move (Bob)
                12:15 to 13:00 checked-in move (Alice), and that it changes nothing (Bob)
                13:00 to 14:00 stale version (Alice), and that it changes nothing (Bob)
                14:00 to 15:30 duplicate booking key (Alice), and that it changes nothing (Bob)
                15:30 to 17:00 audit of a move (Alice), customer move (Bob)

  Background:
    Given the workshop fixture for dealership "MAN01"

  @design-11.3
  Scenario: Moving an appointment to a free time claims the new cells
    Given "Alice" has booked "MOT" for "YK21 ABC" in 8 days at 15:00
    When the appointment is rescheduled to 15:45 in 8 days
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 2
    And the appointment runs from 15:45 to 16:30 in 8 days
    And the appointment is on bay "Bay 2" with technician "Priya"
    And the appointment owns 3 bay cells from 15:45 to 16:30
    And the appointment owns 3 technician cells from 15:45 to 16:30

  Scenario: Moving an appointment appends one audit event
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 15:30
    And the appointment has been rescheduled to 16:15 in 12 days
    When the appointment is fetched with its events
    Then the audit trail has exactly one "RESCHEDULED" event through "ADVISOR_CONSOLE"

  @design-11.3
  Scenario: The time an appointment moved away from can be booked by another customer
    Given "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 08:00
    And the appointment has been rescheduled to 08:45 in 11 days
    When "Bob" books "MOT" for "BD70 XYZ" in 11 days at 08:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  @design-11.3
  Scenario: A move that overlaps the appointment's own cells is claimed
    Given "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 09:30
    When the appointment is rescheduled to 10:00 in 11 days
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 2
    And the appointment is on bay "Bay 2" with technician "Priya"
    And the appointment owns 3 bay cells from 10:00 to 10:45
    And the appointment owns 3 technician cells from 10:00 to 10:45

  @design-11.3
  Scenario: A move that overlaps the appointment's own cells frees the cells it left
    Given "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 11:00
    And the appointment has been rescheduled to 11:30 in 11 days
    When "Bob" books "MOT" for "BD70 XYZ" in 11 days at 10:45
    Then the response is 201 with an id that is a UUID

  @idempotency
  Scenario: A move onto the start of another of the customer's bookings is a duplicate booking
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 14:00
    And the appointment has been cancelled
    And "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 14:45
    When the appointment is rescheduled to 14:00 in 12 days
    Then the response is 409 "DUPLICATE_BOOKING"
    And the problem carries the first booking as the existing appointment with service type "INTERIM"

  @idempotency
  Scenario: A move refused as a duplicate booking keeps the original booking
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 14:00
    And the appointment has been cancelled
    And "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 14:45
    And an attempt to have the appointment rescheduled to 14:00 in 12 days has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 14:45 to 15:30 in 12 days

  @design-11.3
  Scenario: Reassigning the technician at the same time moves the job to the new technician
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 15:15
    And the appointment has been reassigned to bay "Bay 1" with technician "Priya"
    When the appointment is reassigned to technician "Lee"
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 3
    And the appointment runs from 15:15 to 16:00 in 11 days
    And the appointment is on bay "Bay 1" with technician "Lee"
    And the appointment owns 3 technician cells from 15:15 to 16:00

  @design-11.3
  Scenario: Reassigning the technician frees the previous technician for another booking
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 16:00
    And the appointment has been reassigned to bay "Bay 1" with technician "Priya"
    And the appointment has been reassigned to technician "Lee"
    When "Bob" books "MOT" for "BD70 XYZ" in 11 days at 16:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario: A move to a time with no free pair is refused
    Given "Bob" has booked "MOT" for "BD70 XYZ" in 11 days at 13:00
    And "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 12:15
    When the appointment is rescheduled to 13:00 in 11 days
    Then the response is 409 "SLOT_TAKEN"

  Scenario: A move refused for want of a free pair keeps the original booking
    Given "Bob" has booked "MOT" for "BD70 XYZ" in 11 days at 14:30
    And "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 13:45
    And an attempt to have the appointment rescheduled to 14:30 in 11 days has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 13:45 to 14:30 in 11 days
    And the appointment owns 3 bay cells from 13:45 to 14:30

  Scenario: Customers cannot choose the bay or technician when moving an appointment
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 08:00
    When the appointment is reassigned to technician "Lee" through "CUSTOMER_APP"
    Then the response is 403 "FORBIDDEN"

  Scenario: A reassignment refused to a customer channel changes nothing
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 08:00
    And an attempt to have the appointment reassigned to technician "Lee" through "CUSTOMER_APP" has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1

  Scenario: Reassigning to a technician without the required skill is refused
    Given "Alice" has booked "EV_CHECK" for "YK21 ABC" in 8 days at 16:30
    When the appointment is reassigned to technician "Lee"
    Then the response is 422 "NO_QUALIFIED_TECHNICIAN"

  Scenario: A reassignment to a technician without the required skill changes nothing
    Given "Alice" has booked "EV_CHECK" for "YK21 ABC" in 12 days at 08:45
    And an attempt to have the appointment reassigned to technician "Lee" has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1

  Scenario: Reassigning to a bay of a type the service cannot use is refused
    Given "Alice" has booked "MOT" for "YK21 ABC" in 12 days at 09:15
    When the appointment is reassigned to bay "Bay 1"
    Then the response is 422 "INVALID_VALUE" naming the field "serviceBayId"

  Scenario: A reassignment to a bay of a type the service cannot use changes nothing
    Given "Alice" has booked "MOT" for "YK21 ABC" in 12 days at 10:00
    And an attempt to have the appointment reassigned to bay "Bay 1" has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario Outline: A move the booking rules do not allow is refused
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 10:45
    When the appointment is rescheduled to <target>
    Then the response is 422 "<code>"

    Examples:
      | target           | code                  | why                        |
      | 10:50 in 12 days | OFF_GRID_START        | not on the 15-minute grid  |
      | 10:45 yesterday  | INVALID_VALUE         | in the past                |
      | 10:45 in 30 days | INVALID_VALUE         | beyond the booking horizon |
      | 06:00 in 12 days | OUTSIDE_OPENING_HOURS | before the first cell      |

  Scenario Outline: A move the booking rules do not allow keeps the original booking
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 10:45
    And an attempt to have the appointment rescheduled to <target> has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 10:45 to 11:30 in 12 days

    Examples:
      | target           | why                        |
      | 10:50 in 12 days | not on the 15-minute grid  |
      | 10:45 yesterday  | in the past                |
      | 10:45 in 30 days | beyond the booking horizon |
      | 06:00 in 12 days | before the first cell      |

  Scenario: Only a confirmed appointment can be rescheduled
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 12:15
    And the appointment has been checked in
    When the appointment is rescheduled to 12:30 in 12 days
    Then the response is 409 "INVALID_TRANSITION"

  Scenario: A reschedule refused for the appointment's status changes nothing
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 12:15
    And the appointment has been checked in
    And an attempt to have the appointment rescheduled to 12:30 in 12 days has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CHECKED_IN" at version 2
    And the appointment runs from 12:15 to 13:00 in 12 days

  Scenario: A reschedule sent with a stale version is refused
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 13:00
    And the appointment has been rescheduled to 13:15 in 12 days
    When the appointment is rescheduled to 13:00 in 12 days with version 1
    Then the response is 409 "VERSION_CONFLICT"

  Scenario: A reschedule refused for a stale version changes nothing
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 13:00
    And the appointment has been rescheduled to 13:15 in 12 days
    And an attempt to have the appointment rescheduled to 13:00 in 12 days with version 1 has been refused
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 2
    And the appointment runs from 13:15 to 14:00 in 12 days

  Scenario: A customer can move their appointment to another free time
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 15:30 through "CUSTOMER_WEB"
    When the appointment is rescheduled to 16:15 in 12 days through "CUSTOMER_WEB"
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 2
    And the appointment runs from 16:15 to 17:00 in 12 days
    And the record shows no ledger cells and no notes

  Scenario: A customer's move appends one audit event through their channel
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 12 days at 10:45 through "CUSTOMER_WEB"
    And the appointment has been rescheduled to 11:30 in 12 days through "CUSTOMER_WEB"
    When the appointment is fetched with its events
    Then the audit trail has exactly one "RESCHEDULED" event through "CUSTOMER_WEB"
