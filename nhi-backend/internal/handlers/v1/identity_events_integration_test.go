package v1

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Calls all nine sections of the Identity Event Graph for every identity of the
// test tenant, through the real handler, Postgres and ClickHouse (read-only).
// Skips unless NHI_TEST_TENANT_DB / _ID / _USER_ID and ClickHouse are set.
func TestIntegrationIdentityEvents(t *testing.T) {
	h := httpStack(t, true)

	var list struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	env := get(t, h, "/api/v1/identities?page_size=100", http.StatusOK)
	if err := json.Unmarshal(env.Data, &list); err != nil || len(list.Data) == 0 {
		t.Fatalf("identities list: %v (%d rows)", err, len(list.Data))
	}

	sections := []string{"timeline", "credentials", "resources", "heatmap", "sessions", "errors", "cross_account", "ips"}
	var detailChecked bool
	for _, ident := range list.Data {
		base := "/api/v1/identities/" + ident.ID + "/events"
		for _, sec := range sections {
			e := get(t, h, base+"?section="+sec+"&time_range=90d", http.StatusOK)
			m := dataMap(t, e)
			requireKeys(t, ident.Name+"/"+sec, m, "section", "time_range", "data_available")
			var gotSection string
			_ = json.Unmarshal(m["section"], &gotSection)
			if gotSection != sec {
				t.Errorf("%s: section = %q", sec, gotSection)
			}
			var avail bool
			_ = json.Unmarshal(m["data_available"], &avail)
			if !avail {
				t.Errorf("%s/%s: data_available=false", ident.Name, sec)
			}
			if sec == "timeline" {
				var tl struct {
					TotalEvents int `json:"total_events"`
					Events      []struct {
						EventID string `json:"event_id"`
					} `json:"events"`
				}
				_ = json.Unmarshal(e.Data, &tl)
				t.Logf("identity %-24s timeline total_events=%d", ident.Name, tl.TotalEvents)
				if len(tl.Events) > 0 && !detailChecked {
					d := get(t, h, base+"?section=detail&event_id="+tl.Events[0].EventID, http.StatusOK)
					requireKeys(t, "detail", dataMap(t, d), "section", "event", "permission_source", "data_available")
					// The same id must not resolve under a different identity.
					for _, other := range list.Data {
						if other.ID != ident.ID {
							get(t, h, "/api/v1/identities/"+other.ID+"/events?section=detail&event_id="+tl.Events[0].EventID, http.StatusNotFound)
							break
						}
					}
					detailChecked = true
				}
			}
		}
	}
	// Request validation and not-found behaviour through the real stack.
	base := "/api/v1/identities/" + list.Data[0].ID + "/events"
	get(t, h, base+"?section=detail", http.StatusBadRequest)
	get(t, h, base+"?section=detail&event_id=does-not-exist", http.StatusNotFound)
	get(t, h, base+"?time_range=2h", http.StatusBadRequest)
	get(t, h, base+"?page_size=501", http.StatusBadRequest)
	get(t, h, base+"?section=timeline&time_range=1h&service=sts&result=all&read_write=read&page=1&page_size=500", http.StatusOK)
	get(t, h, "/api/v1/identities/00000000-0000-0000-0000-000000000000/events", http.StatusNotFound)
}
