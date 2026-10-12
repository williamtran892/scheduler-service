@spec-001 @claim-ledger @R2 @design-11.3
Feature: The claim ledger
  Capacity is a ledger of 15-minute cells per bay and per technician. A booking claims a run of
  consecutive cells on one bay and one technician, buffers included, and is confirmed only if
  every cell of both runs exists and is free. The appointment keeps the exact minutes promised
  to the customer; only the ledger is quantised. (design 4.2, 5.2, 11.3; notes 4.5)

  These are the rows of design 11.3's validation table that a client can observe. The ledger is
  read only through the cell ranges on the appointment record and through what a second booking
  of the same time is told.
  Calendar: this file books MAN01 only in 3, 4 and 5 days' time, in disjoint windows.

  Background:
    Given the workshop fixture for dealership "MAN01"

  Scenario: A booking claims a run of cells on one bay and one technician
    When "Alice" books "INTERIM" for "YK21 ABC" in 3 days at 09:00
    Then the response is 201 with an id that is a UUID
    And the appointment runs from 09:00 to 09:45 in 3 days
    And the appointment owns 3 bay cells from 09:00 to 09:45
    And the appointment owns 3 technician cells from 09:00 to 09:45
    And the appointment's bay is of type "GENERAL" or "MOT"
    And the appointment's technician is one of "Sam", "Priya" or "Lee"

  Scenario: Buffers are reserved on the ledger but not promised to the customer
    When "Alice" books "MAJOR" for "YK21 ABC" in 3 days at 11:00
    Then the response is 201 with an id that is a UUID
    And the appointment runs from 11:00 to 12:30 in 3 days
    And the appointment is on bay "Bay 1"
    And the appointment owns 8 bay cells from 10:45 to 12:45
    And the appointment owns 8 technician cells from 10:45 to 12:45

  Scenario: A job that is not a whole number of cells is rounded up on the ledger only
    When "Bob" books "DIAGNOSTIC" for "BD70 XYZ" in 3 days at 14:00
    Then the response is 201 with an id that is a UUID
    And the appointment runs from 14:00 to 14:50 in 3 days
    And the appointment owns 4 bay cells from 14:00 to 15:00
    And the appointment owns 4 technician cells from 14:00 to 15:00

  Scenario: Back-to-back bookings on the same pair are both confirmed
    Given "Alice" has booked "MOT" for "YK21 ABC" in 4 days at 09:00
    When "Bob" books "MOT" for "BD70 XYZ" in 4 days at 09:45
    Then the response is 201 with an id that is a UUID
    And the appointment is on bay "Bay 2" with technician "Priya"
    And the appointment owns 3 bay cells from 09:45 to 10:30

  Scenario: Overlapping windows on the only qualified pair conflict
    Given "Alice" has booked "MOT" for "YK21 ABC" in 4 days at 12:00
    When "Bob" books "MOT" for "BD70 XYZ" in 4 days at 12:30
    Then the response is 409 "SLOT_TAKEN"

  Scenario: A busy bay blocks the booking even when qualified technicians are free
    Given "Alice" has booked "MAJOR" for "YK21 ABC" in 4 days at 14:00
    When "Bob" books "MAJOR" for "BD70 XYZ" in 4 days at 14:00
    Then the response is 409 "SLOT_TAKEN"

  Scenario: A free bay is not enough when the only qualified technician is busy
    Given "Alice" has booked "EV_MAJOR" for "YK21 ABC" in 5 days at 10:00
    And the appointment is on bay "Bay 3" with technician "Sam"
    When "Bob" books "EV_CHECK" for "BD70 XYZ" in 5 days at 10:30
    Then the response is 409 "SLOT_TAKEN"

  Scenario: The same window on a different pair is confirmed
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 5 days at 14:00
    When "Bob" books "INTERIM" for "BD70 XYZ" in 5 days at 14:00
    Then the response is 201 with an id that is a UUID
    And the two appointments hold different bays and different technicians
