package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestScheduleCRUDAndPermissions(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"sched"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// Create a schedule.
	_, s := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), `{"cron":"0 3 * * *"}`, http.StatusCreated)
	scheduleID := uint(s["id"].(float64))
	if s["enabled"].(bool) != true {
		t.Fatalf("new schedule should be enabled, got %v", s)
	}

	// List schedules.
	_, out := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), "", http.StatusOK)
	list := out["schedules"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(list))
	}

	// Invalid cron rejected.
	c.do("POST", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), `{"cron":"not-a-cron"}`, http.StatusBadRequest)

	// Update cron.
	_, updated := c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d/schedules/%d", flowID, scheduleID), `{"cron":"0 4 * * *"}`, http.StatusOK)
	if updated["cron"].(string) != "0 4 * * *" {
		t.Fatalf("cron not updated: %v", updated)
	}

	// Pause.
	_, paused := c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d/schedules/%d/enabled", flowID, scheduleID), `{"enabled":false}`, http.StatusOK)
	if paused["enabled"].(bool) != false {
		t.Fatalf("schedule should be paused, got %v", paused)
	}

	// Resume.
	_, resumed := c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d/schedules/%d/enabled", flowID, scheduleID), `{"enabled":true}`, http.StatusOK)
	if resumed["enabled"].(bool) != true {
		t.Fatalf("schedule should be resumed, got %v", resumed)
	}

	// A second, unprivileged user cannot manage schedules.
	c2 := &client{t: t, ts: c.ts}
	c2.registerAndLoginAs("bob")
	c2.do("GET", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), "", http.StatusForbidden)
	c2.do("POST", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), `{"cron":"0 3 * * *"}`, http.StatusForbidden)
	c2.do("PATCH", fmt.Sprintf("/api/flow/flows/%d/schedules/%d/enabled", flowID, scheduleID), `{"enabled":false}`, http.StatusForbidden)
	c2.do("DELETE", fmt.Sprintf("/api/flow/flows/%d/schedules/%d", flowID, scheduleID), "", http.StatusForbidden)

	// A read-only member may list but not modify.
	reader := &client{t: t, ts: c.ts}
	readerID := reader.registerAndLoginAs("reader")
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/members", testSetID),
		fmt.Sprintf(`{"user_id":%d,"role":"read"}`, readerID), http.StatusCreated)
	reader.do("GET", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), "", http.StatusOK)
	reader.do("POST", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), `{"cron":"0 3 * * *"}`, http.StatusForbidden)

	// Owner deletes the schedule.
	_, del := c.do("DELETE", fmt.Sprintf("/api/flow/flows/%d/schedules/%d", flowID, scheduleID), "", http.StatusOK)
	if del["deleted"].(bool) != true {
		t.Fatalf("expected deleted, got %v", del)
	}
	c.do("GET", fmt.Sprintf("/api/flow/flows/%d/schedules", flowID), "", http.StatusOK)
}
