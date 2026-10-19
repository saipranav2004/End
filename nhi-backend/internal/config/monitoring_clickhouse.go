package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MonitoringClickHouse returns the connection settings of the behaviour
// pipeline's ClickHouse event store (current_pipeline_schema_guide.md §14,
// table log_events). It is read only from the environment, under its own
// prefix, so it never collides with the legacy NHI_CLICKHOUSE_* settings:
//
//	NHI_MONITORING_CLICKHOUSE_HOST              required to enable; empty = disabled
//	NHI_MONITORING_CLICKHOUSE_PORT              default 8123 (http) / 9000 (native)
//	NHI_MONITORING_CLICKHOUSE_PROTOCOL          http (default) | native
//	NHI_MONITORING_CLICKHOUSE_DATABASE          required, e.g. Bimasugam_ClickHouse
//	NHI_MONITORING_CLICKHOUSE_TABLE             default log_events (the pipeline's CLICKHOUSE_EVENTS_TABLE)
//	NHI_MONITORING_CLICKHOUSE_USER / _PASSWORD
//	NHI_MONITORING_CLICKHOUSE_SECURE            true | false (TLS), default false
//	NHI_MONITORING_CLICKHOUSE_TIMEOUT_SECONDS   default 5
//
// When the host is empty the returned config reports Enabled() == false and
// the ClickHouse-backed dashboard fields stay null.
func MonitoringClickHouse() (ClickHouseConfig, error) {
	const prefix = "NHI_MONITORING_CLICKHOUSE_"
	setString := func(dst *string, key string) {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			*dst = strings.TrimSpace(value)
		}
	}
	setInt := func(dst *int, key string) error {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("config: %s must be an integer", key)
			}
			*dst = parsed
		}
		return nil
	}

	var c ClickHouseConfig
	setString(&c.Host, prefix+"HOST")
	if !c.Enabled() {
		return c, nil
	}
	if err := setInt(&c.Port, prefix+"PORT"); err != nil {
		return c, err
	}
	setString(&c.Database, prefix+"DATABASE")
	setString(&c.User, prefix+"USER")
	setString(&c.Password, prefix+"PASSWORD")
	setString(&c.Table, prefix+"TABLE")
	setString(&c.Protocol, prefix+"PROTOCOL")
	if err := setInt(&c.TimeoutSeconds, prefix+"TIMEOUT_SECONDS"); err != nil {
		return c, err
	}
	if value, ok := os.LookupEnv(prefix + "SECURE"); ok && strings.TrimSpace(value) != "" {
		secure, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return c, fmt.Errorf("config: %sSECURE must be true or false", prefix)
		}
		c.Secure = secure
	}

	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))
	if c.Protocol == "" {
		c.Protocol = "http"
	}
	if c.Port == 0 {
		if c.Protocol == "http" {
			c.Port = 8123
		} else {
			c.Port = 9000
		}
	}
	if strings.TrimSpace(c.Table) == "" {
		c.Table = "log_events"
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 5
	}

	switch {
	case c.Port < 1 || c.Port > 65535:
		return c, fmt.Errorf("config: %sPORT must be between 1 and 65535", prefix)
	case strings.TrimSpace(c.Database) == "" || !databaseNameRE.MatchString(c.Database):
		return c, fmt.Errorf("config: %sDATABASE must be a valid identifier", prefix)
	case !databaseNameRE.MatchString(c.Table): // interpolated into SQL, so it must be a plain identifier
		return c, fmt.Errorf("config: %sTABLE must be a valid identifier", prefix)
	case c.Protocol != "native" && c.Protocol != "http":
		return c, fmt.Errorf("config: %sPROTOCOL must be native or http", prefix)
	case c.TimeoutSeconds < 1 || c.TimeoutSeconds > 60:
		return c, fmt.Errorf("config: %sTIMEOUT_SECONDS must be between 1 and 60", prefix)
	}
	return c, nil
}
