@spec-002 @no-show @R3 @design-4.3
Feature: No-shows and late arrivals
  A confirmed appointment nobody checked in is marked a no-show once its start plus the grace
  period (30 minutes by default) has passed, by a sweeper that runs inside the service on an
  interval, and every cell it owns is released. A no-show who turns up late is checked in onto
  the cells free from the current grid cell, in the same step, or refused when nothing is free.
  A job completed after its booked time keeps the cells already in the past. (design 4.3, 5.6,
  11.4 "Cancel / no-show", "Complete"; notes 4.7; markNoShow, checkInAppointment,
  completeAppointment)

  These scenarios depend on the real time. They book dealership LDS01, whose one bay and one
  technician have ledger cells around the clock, so the current grid cell always exists.
  Scenarios that need an appointment's time to have passed move it, and the cells it owns, 14
  days into the past through the suite's database connection, the only write the suite makes,
  and then poll the public record until a deadline. In the test configuration the sweeper runs
  about every second.
  Calendar: this file books LDS01 only in 9 days' time (18:00 to 22:45), in 10 days' time
  (18:00 to 21:30) and in 11 days' time (18:00 to 18:45), one window per scenario, and today
  around the current time. These are evening hours, when MAN01 has no cells, so no booking key
  here (vehicle, customer, start, kind) can collide with one another feature file books for the
  same customer at MAN01. Backdated windows lose their cells and are never booked again.
    today:      the accepted late arrival claims the three cells from the current grid cell; the
                refused late arrivals are MAJOR jobs (eight cells with their buffers), refused
                because a booking 45 or 90 minutes after the current grid cell holds the pair, so
                none of the three needs a cell another one holds
    in 9 days:  18:00 to 18:45 accepted late arrival; 18:45 to 20:45 refused late arrival;
                20:45 to 22:45 refused late arrival changes nothing
    in 10 days: 18:00 to 18:45 sweeper marks a no-show; 19:00 to 19:45 its audit event;
                20:00 to 21:30 sweeper leaves a checked-in appointment
    in 11 days: 18:00 to 18:45 completion after the booked time

  Background:
    Given the workshop fixture for dealership "LDS01"

  @clock
  Scenario: A late arrival is checked in onto the cells free now
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 18:00 at dealership "LDS01"
    And the appointment has been marked a no-show
    When the appointment is checked in
    Then the response is 200
    And the appointment status is "CHECKED_IN" at version 3
    And the appointment starts at the current grid cell
    And the appointment is on bay "Bay L1" with technician "Jo"
    And the appointment owns 3 bay cells and 3 technician cells

  @clock
  Scenario: A late arrival is refused when the cells from now are not free
    Given "Customer 01" has booked "INTERIM" for "TEST 01" 45 minutes after the current grid cell at dealership "LDS01"
    And "Bob" has booked "MAJOR" for "BD70 XYZ" in 9 days at 19:00 at dealership "LDS01"
    And the appointment has been marked a no-show
    When the appointment of "Bob" is checked in
    Then the response is 409 "SLOT_TAKEN"

  @clock
  Scenario: A late arrival that is refused leaves the no-show unchanged
    Given "Customer 02" has booked "INTERIM" for "TEST 02" 90 minutes after the current grid cell at dealership "LDS01"
    And "Bob" has booked "MAJOR" for "BD70 XYZ" in 9 days at 21:00 at dealership "LDS01"
    And the appointment has been marked a no-show
    And an attempt to have the appointment of "Bob" checked in has been refused
    When the appointment of "Bob" is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "NO_SHOW" at version 2

  @clock
  Scenario: The sweeper marks a confirmed appointment as a no-show once its grace period has passed
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 18:00 at dealership "LDS01"
    When the appointment is backdated by 14 days
    Then the appointment becomes "NO_SHOW" within 10 seconds
    And the appointment status is "NO_SHOW" at version 2
    And the appointment owns no ledger cells

  @clock
  Scenario: The sweeper's no-show appends one audit event by the sweeper
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 19:00 at dealership "LDS01"
    And the appointment has been backdated by 14 days
    And the appointment has become "NO_SHOW" within 10 seconds
    When the appointment is fetched with its events
    Then the audit trail has exactly one "NO_SHOW" event by "no-show-sweeper"

  @clock
  Scenario: The sweeper leaves an appointment that has been checked in
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 10 days at 20:00 at dealership "LDS01"
    And the appointment has been checked in
    And the appointment has been backdated by 14 days
    And "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 20:45 at dealership "LDS01"
    And the appointment of "Alice" has been backdated by 14 days
    And the appointment of "Alice" has become "NO_SHOW" within 10 seconds
    When the appointment of "Bob" is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CHECKED_IN" at version 2
    And the appointment owns 3 bay cells and 3 technician cells

  @clock @design-11.3
  Scenario: Completing a job after its booked time keeps the cells already in the past
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 18:00 at dealership "LDS01"
    And the appointment has been checked in
    And the appointment has been started
    And the appointment has been backdated by 14 days
    When the appointment is completed
    Then the response is 200
    And the appointment status is "COMPLETED" at version 4
    And the appointment has an actual start time and an actual end time
    And the appointment owns 3 bay cells and 3 technician cells
