@spec-001 @idempotency @R3
Feature: Repeating a booking is safe
  A booking is identified by its vehicle, customer, start time and kind. At most one appointment
  exists per booking key, so a retry after a lost response or a double submit returns the one
  appointment that exists. The client sends no idempotency header. (design 4.1.4, 5.2, 7, 11.1;
  notes 0 D4, 4.4; bookAppointment "Safe to repeat")

  Calendar: this file books MAN01 only in 6 days' time (09:00 to 16:30, in six disjoint windows)
  and in 7 days' time (10:00 to 10:45).

  Background:
    Given the workshop fixture for dealership "MAN01"

  Scenario: Submitting the same booking again returns the same appointment
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 6 days at 09:00
    When "Alice" books "INTERIM" for "YK21 ABC" in 6 days at 09:00
    Then the response is 201 with the same appointment as the first booking
    And the appointment status is "CONFIRMED" at version 1
    And the Location header points at that appointment

  Scenario: Resubmitting a booking whose only pair it already holds returns it
    Given "Alice" has booked "MOT" for "YK21 ABC" in 6 days at 10:30
    When "Alice" books "MOT" for "YK21 ABC" in 6 days at 10:30
    Then the response is 201 with the same appointment as the first booking

  Scenario: Resubmitting with different customer requests returns the original unchanged
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 6 days at 12:00 with:
      | requestNotes | Rattle from rear left |
    When "Alice" books "INTERIM" for "YK21 ABC" in 6 days at 12:00 with:
      | requestNotes | Also check the wipers |
    Then the response is 201 with the same appointment as the first booking
    And the appointment has customer requests "Rattle from rear left"

  Scenario: The same booking key with a different service type is a duplicate booking
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 6 days at 13:30
    When "Alice" books "MAJOR" for "YK21 ABC" in 6 days at 13:30
    Then the response is 409 "DUPLICATE_BOOKING"
    And the problem carries the first booking as the existing appointment with service type "INTERIM"

  Scenario: The same booking key at another dealership is a duplicate booking
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 6 days at 14:30
    When "Alice" books "INTERIM" for "YK21 ABC" in 6 days at 14:30 at dealership "LDS01"
    Then the response is 409 "DUPLICATE_BOOKING"
    And the problem carries the first booking as the existing appointment with service type "INTERIM"

  Scenario: A request that failed leaves nothing behind, so its retry runs again
    Given "Alice" has attempted to book "DIAGNOSTIC" for "YK21 ABC" in 6 days at 15:30 through "CUSTOMER_WEB" and been refused
    When "Alice" books "DIAGNOSTIC" for "YK21 ABC" in 6 days at 15:30 through "ADVISOR_CONSOLE"
    Then the response is 201 with an id that is a UUID

  @slow
  Scenario: Simultaneous identical submissions create one appointment
    When "Alice" submits the same booking of "MOT" for "YK21 ABC" in 7 days at 10:00 10 times at the same time
    Then all 10 responses are 201 with the same appointment id
