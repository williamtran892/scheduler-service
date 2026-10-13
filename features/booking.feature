@spec-001 @booking @R1 @R3
Feature: Book an appointment
  A client asks for a service at a dealership for a customer's vehicle at a start time.
  The service chooses a compatible bay and a qualified technician that are both free for the
  whole job, confirms the appointment in one transaction, and returns the record. The record's
  id is the reference quoted to the customer. (design 5.2, 11.4 "Book"; bookAppointment,
  getAppointment)

  Dates are relative to the day the suite runs and times are Europe/London. The database is not
  reset between scenarios, so each scenario books its own window.
  Calendar: this file books MAN01 only tomorrow (08:45 to 16:45) and in 2 days' time.

  Background:
    Given the workshop fixture for dealership "MAN01"

  Scenario: Booking an appointment confirms it with the full record
    When "Alice" books "EV_MAJOR" for "YK21 ABC" tomorrow at 09:00 with:
      | kind         | DROP_OFF                                    |
      | waiting      | false                                       |
      | courtesyCar  | true                                        |
      | requestNotes | Charging port cover loose                   |
      | notes        | Customer prefers a morning call when ready. |
    Then the response is 201 with an id that is a UUID
    And the Location header points at that appointment
    And the appointment status is "CONFIRMED" at version 1
    And the appointment runs from 09:00 to 11:00 tomorrow
    And the appointment is for "Alice" with vehicle "YK21 ABC", service type "EV_MAJOR" at dealership "MAN01"
    And the appointment is on bay "Bay 3" with technician "Sam"
    And the appointment owns 10 bay cells from 08:45 to 11:15
    And the appointment owns 10 technician cells from 08:45 to 11:15
    And the appointment records the kind "DROP_OFF" through "ADVISOR_CONSOLE"
    And the appointment has customer requests "Charging port cover loose" with a courtesy car
    And the appointment has the notes "Customer prefers a morning call when ready."

  Scenario: The confirmed appointment can be read back with its audit trail
    Given "Alice" has booked "INTERIM" for "YK21 ABC" tomorrow at 11:30
    When the appointment is fetched with its events
    Then the response is 200
    And the fetched record equals the booking response
    And the audit trail has exactly one "CONFIRMED" event through "ADVISOR_CONSOLE"

  Scenario: A customer channel sees its booking without ledger cells, internal notes or technician name
    When "Bob" books "INTERIM" for "BD70 XYZ" tomorrow at 13:00 through "CUSTOMER_WEB" with:
      | requestNotes | Rattle from rear left   |
      | notes        | Internal: offer a valet |
    Then the response is 201 with an id that is a UUID
    And the appointment records the kind "DROP_OFF" through "CUSTOMER_WEB"
    And the appointment has customer requests "Rattle from rear left"
    And the record shows no ledger cells and no notes
    And the record shows the technician by id only
    When the appointment is fetched through "ADVISOR_CONSOLE"
    Then the response is 200
    And the appointment owns 3 bay cells from 13:00 to 13:45
    And the record shows no notes

  Scenario: The least-loaded qualified bay and technician get the job
    Given "Alice" has booked "EV_MAJOR" for "YK21 ABC" in 2 days at 09:00
    And the appointment is on bay "Bay 3" with technician "Sam"
    And "Bob" has booked "MOT" for "BD70 XYZ" in 2 days at 09:00
    And the appointment is on bay "Bay 2" with technician "Priya"
    When "Alice" books "INTERIM" for "YK21 ABC" in 2 days at 13:00
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 1" with technician "Lee"

  @R2
  Scenario: When every qualified pair is taken the booking is refused
    Given "Alice" has booked "MOT" for "YK21 ABC" tomorrow at 15:00
    When "Bob" books "MOT" for "BD70 XYZ" tomorrow at 15:00
    Then the response is 409 "SLOT_TAKEN"

  Scenario: An unknown appointment is not found
    When appointment "00000000-0000-4000-8000-000000000000" is fetched
    Then the response is 404 "NOT_FOUND"

  Scenario: A start off the booking grid is rejected
    When "Alice" books "INTERIM" for "YK21 ABC" tomorrow at 09:05
    Then the response is 422 "OFF_GRID_START"

  Scenario Outline: A booking outside opening hours is rejected
    When "Alice" books "<service>" for "YK21 ABC" tomorrow at <start>
    Then the response is 422 "OUTSIDE_OPENING_HOURS"

    Examples:
      | service | start | why                                         |
      | INTERIM | 06:00 | before the first cell                       |
      | INTERIM | 19:00 | after the last cell                         |
      | MAJOR   | 17:00 | the job and its after-buffer run past close |

  Scenario: A service no technician is currently qualified for is rejected
    When "Alice" books "AIRCON" for "YK21 ABC" tomorrow at 12:00
    Then the response is 422 "NO_QUALIFIED_TECHNICIAN"

  Scenario Outline: A staff-only service cannot be booked from a customer channel
    When "Alice" books "DIAGNOSTIC" for "YK21 ABC" tomorrow at 12:00 through "<channel>"
    Then the response is 422 "SERVICE_NOT_BOOKABLE_ONLINE"

    Examples:
      | channel      |
      | CUSTOMER_WEB |
      | CUSTOMER_APP |

  Scenario Outline: A malformed or inconsistent booking request is rejected
    Given "Alice" prepares a booking of "INTERIM" for "<vehicle>" <day> at 12:00
    And the request has <change>
    When the booking request is sent
    Then the response is <status> "<code>" naming the field "<field>"

    Examples:
      | vehicle  | day        | change                                                      | status | code             | field         |
      | YK21 ABC | tomorrow   | "serviceTypeId" removed                                     | 400    | VALIDATION_ERROR | serviceTypeId |
      | YK21 ABC | tomorrow   | "startAt" set to "2026-11-02T12:00:00"                      | 400    | VALIDATION_ERROR | startAt       |
      | YK21 ABC | tomorrow   | no channel                                                  | 400    | VALIDATION_ERROR | X-Channel     |
      | YK21 ABC | tomorrow   | "kind" set to "PICKUP"                                      | 422    | INVALID_VALUE    | kind          |
      | YK21 ABC | tomorrow   | "serviceTypeId" set to "00000000-0000-4000-8000-000000000000" | 422    | INVALID_VALUE    | serviceTypeId |
      | BD70 XYZ | tomorrow   | no changes                                                  | 422    | INVALID_VALUE    | vehicleId     |
      | YK21 ABC | yesterday  | no changes                                                  | 422    | INVALID_VALUE    | startAt       |
      | YK21 ABC | in 30 days | no changes                                                  | 422    | INVALID_VALUE    | startAt       |
