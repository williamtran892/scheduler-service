@spec-001 @concurrency @R2
Feature: Concurrent bookings never double book
  Two requests can never both hold the same bay or technician for overlapping cells, with no
  distributed lock and at MySQL's default isolation level. Racing requests are fired in parallel
  from the suite and their outcomes counted; which request wins is not asserted, because the
  database decides. (design 5.2, 5.3, 7, 11.3; notes 4.5)

  Calendar: this file books MAN01 only in 7 days' time (13:00 to 15:45) and in 8 days' time.

  Background:
    Given the workshop fixture for dealership "MAN01"

  @slow @design-11.3
  Scenario: Twenty customers race for the only MOT pair
    When customers "Customer 01" to "Customer 20" each book "MOT" for their own vehicle in 7 days at 13:00 at the same time
    Then exactly 1 booking succeeded and 19 received 409 "SLOT_TAKEN"
    And the successful appointment is on bay "Bay 2" with technician "Priya"
    And the successful appointment owns 3 bay cells from 13:00 to 13:45
    And the successful appointment owns 3 technician cells from 13:00 to 13:45

  @design-5.3
  Scenario: Two customers, one slot, two pairs
    When "Alice" and "Bob" each book "INTERIM" for their own vehicle in 7 days at 15:00 at the same time
    Then both bookings succeeded
    And the two appointments hold different bays and different technicians

  @slow @design-11.3
  Scenario: Simultaneous bookings of different hours on the same pair do not interfere
    When these customers each book "MOT" for their own vehicle in 8 days at the same time:
      | customer    | start |
      | Customer 01 | 09:00 |
      | Customer 02 | 09:45 |
      | Customer 03 | 10:30 |
      | Customer 04 | 11:15 |
      | Customer 05 | 12:00 |
      | Customer 06 | 12:45 |
      | Customer 07 | 13:30 |
      | Customer 08 | 14:15 |
    Then all 8 bookings succeeded on bay "Bay 2" with technician "Priya"
