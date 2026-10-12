@bootstrap
Feature: Service health
  The service reports whether it can serve requests, so an orchestrator
  knows when to route traffic to it.

  Scenario: The service is healthy when its database is reachable
    When I send a GET request to "/healthz"
    Then the response status is 200
    And the response JSON field "status" is "ok"

  Scenario: An unknown route is not found
    When I send a GET request to "/no-such-route"
    Then the response status is 404
