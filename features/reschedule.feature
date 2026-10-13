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
  Calendar: this file books MAN01 only in 11 days' time (09:00 to 16:45) and in 12 days' time
  (08:00 to 16:45).

  Background:
    Given the workshop fixture for dealership "MAN01"

  @design-11.3
  Scenario: Moving an appointment to a free time claims the new cells and frees the old ones
    Given "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 09:00
    When the appointment is rescheduled to 10:00 in 11 days
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 2
    And the appointment runs from 10:00 to 10:45 in 11 days
    And the appointment is on bay "Bay 2" with technician "Priya"
    And the appointment owns 3 bay cells from 10:00 to 10:45
    And the appointment owns 3 technician cells from 10:00 to 10:45
    When the appointment is fetched with its events
    Then the audit trail has exactly one "RESCHEDULED" event through "ADVISOR_CONSOLE"
    When "Bob" books "MOT" for "BD70 XYZ" in 11 days at 09:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  @design-11.3
  Scenario: A move that overlaps the appointment's own cells is claimed
    Given "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 11:00
    When the appointment is rescheduled to 11:30 in 11 days
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 2
    And the appointment is on bay "Bay 2" with technician "Priya"
    And the appointment owns 3 bay cells from 11:30 to 12:15
    And the appointment owns 3 technician cells from 11:30 to 12:15
    When "Bob" books "MOT" for "BD70 XYZ" in 11 days at 10:45
    Then the response is 201 with an id that is a UUID

  @idempotency
  Scenario: A move onto the start of another of the customer's bookings is a duplicate booking
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 12:30
    And the appointment has been cancelled
    And "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 13:15
    When the appointment is rescheduled to 12:30 in 11 days
    Then the response is 409 "DUPLICATE_BOOKING"
    And the problem carries the first booking as the existing appointment with service type "INTERIM"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 13:15 to 14:00 in 11 days

  @design-11.3
  Scenario: Reassigning the technician at the same time frees the previous technician
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 14:00
    And the appointment has been reassigned to bay "Bay 1" with technician "Priya"
    When the appointment is reassigned to technician "Lee"
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 3
    And the appointment runs from 14:00 to 14:45 in 11 days
    And the appointment is on bay "Bay 1" with technician "Lee"
    And the appointment owns 3 technician cells from 14:00 to 14:45
    When "Bob" books "MOT" for "BD70 XYZ" in 11 days at 14:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario: A move to a time with no free pair is refused and the original booking is kept
    Given "Bob" has booked "MOT" for "BD70 XYZ" in 11 days at 16:00
    And "Alice" has booked "MOT" for "YK21 ABC" in 11 days at 15:00
    When the appointment is rescheduled to 16:00 in 11 days
    Then the response is 409 "SLOT_TAKEN" with between 1 and 5 alternatives
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 15:00 to 15:45 in 11 days
    And the appointment owns 3 bay cells from 15:00 to 15:45

  Scenario: Customers cannot choose the bay or technician when moving an appointment
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 08:00
    When the appointment is reassigned to technician "Lee" through "CUSTOMER_APP"
    Then the response is 403 "FORBIDDEN"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1

  Scenario: Reassigning to a technician without the required skill is refused
    Given "Alice" has booked "EV_CHECK" for "YK21 ABC" in 12 days at 09:00
    When the appointment is reassigned to technician "Lee"
    Then the response is 422 "NO_QUALIFIED_TECHNICIAN"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1

  Scenario: Reassigning to a bay of a type the service cannot use is refused
    Given "Alice" has booked "MOT" for "YK21 ABC" in 12 days at 10:00
    When the appointment is reassigned to bay "Bay 1"
    Then the response is 422 "INVALID_VALUE" naming the field "serviceBayId"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment is on bay "Bay 2" with technician "Priya"

  Scenario Outline: A move the booking rules do not allow is refused
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 11:00
    When the appointment is rescheduled to <target>
    Then the response is 422 "<code>"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 11:00 to 11:45 in 12 days

    Examples:
      | target           | code                  | why                        |
      | 11:05 in 12 days | OFF_GRID_START        | not on the 15-minute grid  |
      | 11:00 yesterday  | INVALID_VALUE         | in the past                |
      | 11:00 in 30 days | INVALID_VALUE         | beyond the booking horizon |
      | 06:00 in 12 days | OUTSIDE_OPENING_HOURS | before the first cell      |

  Scenario: Only a confirmed appointment can be rescheduled
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 12:00
    And the appointment has been checked in
    When the appointment is rescheduled to 12:30 in 12 days
    Then the response is 409 "INVALID_TRANSITION"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CHECKED_IN" at version 2
    And the appointment runs from 12:00 to 12:45 in 12 days

  Scenario: A reschedule sent with a stale version is refused
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 12 days at 13:00
    And the appointment has been rescheduled to 13:30 in 12 days
    When the appointment is rescheduled to 14:00 in 12 days with version 1
    Then the response is 409 "VERSION_CONFLICT"
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CONFIRMED" at version 2
    And the appointment runs from 13:30 to 14:15 in 12 days

  Scenario: A customer can move their appointment to another free time
    When "Alice" books "INTERIM" for "YK21 ABC" in 12 days at 15:00 through "CUSTOMER_WEB"
    And the appointment is rescheduled to 16:00 in 12 days through "CUSTOMER_WEB"
    Then the response is 200
    And the appointment status is "CONFIRMED" at version 2
    And the appointment runs from 16:00 to 16:45 in 12 days
    And the record shows no ledger cells and no notes
    When the appointment is fetched with its events
    Then the audit trail has exactly one "RESCHEDULED" event through "CUSTOMER_WEB"
