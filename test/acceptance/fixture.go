package acceptance

import "fmt"

// The fixed ids of local/mysql/data.sql, by the names the feature files use. Keep in step with
// that file; specs/001-book-appointment/data-model.md lists the same rows.

var dealerships = map[string]string{
	"MAN01": "d0000000-0000-4000-8000-000000000001",
	"LDS01": "d0000000-0000-4000-8000-000000000002",
}

var bays = map[string]string{
	"Bay 1": "b0000000-0000-4000-8000-000000000001",
	"Bay 2": "b0000000-0000-4000-8000-000000000002",
	"Bay 3": "b0000000-0000-4000-8000-000000000003",
}

var technicians = map[string]string{
	"Sam":   "e0000000-0000-4000-8000-000000000001",
	"Priya": "e0000000-0000-4000-8000-000000000002",
	"Lee":   "e0000000-0000-4000-8000-000000000003",
}

var serviceTypes = map[string]string{
	"INTERIM":    "5e000000-0000-4000-8000-000000000001",
	"MAJOR":      "5e000000-0000-4000-8000-000000000002",
	"MOT":        "5e000000-0000-4000-8000-000000000003",
	"EV_MAJOR":   "5e000000-0000-4000-8000-000000000004",
	"EV_CHECK":   "5e000000-0000-4000-8000-000000000005",
	"DIAGNOSTIC": "5e000000-0000-4000-8000-000000000006",
	"AIRCON":     "5e000000-0000-4000-8000-000000000007",
}

// customer is a fixture customer and the one vehicle they own.
type customer struct {
	id, vehicleID, registration string
}

var customers = func() map[string]customer {
	m := map[string]customer{
		"Alice": {"c0000000-0000-4000-8000-000000000001", "f0000000-0000-4000-8000-000000000001", "YK21 ABC"},
		"Bob":   {"c0000000-0000-4000-8000-000000000002", "f0000000-0000-4000-8000-000000000002", "BD70 XYZ"},
	}
	for i := 1; i <= 20; i++ { // Customer 01 to Customer 20, ids ending 101 to 120
		m[fmt.Sprintf("Customer %02d", i)] = customer{
			id:           fmt.Sprintf("c0000000-0000-4000-8000-000000000%d", 100+i),
			vehicleID:    fmt.Sprintf("f0000000-0000-4000-8000-000000000%d", 100+i),
			registration: fmt.Sprintf("TEST %02d", i),
		}
	}
	return m
}()

// vehicles maps a registration to its vehicle id.
var vehicles = func() map[string]string {
	m := map[string]string{}
	for _, c := range customers {
		m[c.registration] = c.vehicleID
	}
	return m
}()

// lookup returns m[name] or an error naming the kind of thing that is unknown.
func lookup(kind string, m map[string]string, name string) (string, error) {
	id, ok := m[name]
	if !ok {
		return "", fmt.Errorf("unknown %s %q: not in the fixture (local/mysql/data.sql)", kind, name)
	}
	return id, nil
}

// nameOf returns the fixture name for id in m, or the id itself.
func nameOf(m map[string]string, id string) string {
	for name, v := range m {
		if v == id {
			return name
		}
	}
	return id
}

func customerNamed(name string) (customer, error) {
	c, ok := customers[name]
	if !ok {
		return customer{}, fmt.Errorf("unknown customer %q: not in the fixture (local/mysql/data.sql)", name)
	}
	return c, nil
}
