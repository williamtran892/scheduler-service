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
  Calendar: this file books LDS01 only in 9 days' time (18:00 to 19:45), in 10 days' time
  (18:00 to 20:30) and in 11 days' time (18:00 to 18:45); the late arrival takes LDS01's cells
  at the current time. These are evening hours, when MAN01 has no cells, so no booking key here
  (vehicle, customer, start, kind) can collide with one another feature file books for the same
  customer at MAN01. Backdated windows lose their cells and are never booked again.

  Background:
    Given the workshop fixture for dealership "LDS01"

  @clock
  Scenario: A late arrival is checked in onto the cells free now, and refused when none are
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 9 days at 18:00 at dealership "LDS01"
    And the appointment has been marked a no-show
    And "Bob" has booked "INTERIM" for "BD70 XYZ" in 9 days at 19:00 at dealership "LDS01"
    And the appointment has been marked a no-show
    When the appointment of "Alice" is checked in
    Then the response is 200
    And the appointment status is "CHECKED_IN" at version 3
    And the appointment starts at the current grid cell
    And the appointment is on bay "Bay L1" with technician "Jo"
    And the appointment owns 3 bay cells and 3 technician cells
    When the appointment of "Bob" is checked in
    Then the response is 409 "SLOT_TAKEN" with between 1 and 5 alternatives
    When the appointment of "Bob" is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "NO_SHOW" at version 2

  @clock
  Scenario: The sweeper marks a confirmed appointment as a no-show once its grace period has passed
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 18:00 at dealership "LDS01"
    When the appointment is backdated by 14 days
    Then the appointment becomes "NO_SHOW" within 10 seconds
    And the appointment status is "NO_SHOW" at version 2
    And the appointment owns no ledger cells
    When the appointment is fetched with its events
    Then the audit trail has exactly one "NO_SHOW" event by "no-show-sweeper"

  @clock
  Scenario: The sweeper leaves an appointment that has been checked in
    Given "Bob" has booked "INTERIM" for "BD70 XYZ" in 10 days at 19:00 at dealership "LDS01"
    And the appointment has been checked in
    And the appointment is backdated by 14 days
    And "Alice" has booked "INTERIM" for "YK21 ABC" in 10 days at 19:45 at dealership "LDS01"
    When the appointment of "Alice" is backdated by 14 days
    Then the appointment of "Alice" becomes "NO_SHOW" within 10 seconds
    When the appointment of "Bob" is fetched through "ADVISOR_CONSOLE"
    Then the appointment status is "CHECKED_IN" at version 2
    And the appointment owns 3 bay cells and 3 technician cells

  @clock @design-11.3
  Scenario: Completing a job after its booked time keeps the cells already in the past
    Given "Alice" has booked "INTERIM" for "YK21 ABC" in 11 days at 18:00 at dealership "LDS01"
    And the appointment has been checked in
    And the appointment has been started
    And the appointment is backdated by 14 days
    When the appointment is completed
    Then the response is 200
    And the appointment status is "COMPLETED" at version 4
    And the appointment has an actual start time and an actual end time
    And the appointment owns 3 bay cells and 3 technician cells
