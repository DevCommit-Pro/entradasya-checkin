package checkin

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                                  int
	JWTSecret, InternalAPIKey, TicketsURL string
	HTTPTimeout, OperationTimeout         time.Duration
}

func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 || len(c.JWTSecret) < 32 || len(c.InternalAPIKey) < 32 || c.HTTPTimeout <= 0 || c.HTTPTimeout > 3*time.Second || c.OperationTimeout <= 0 || c.OperationTimeout > 6*time.Second {
		return errors.New("puerto, secretos o timeouts no válidos")
	}
	u, err := url.Parse(c.TicketsURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.TicketsURL, "#") {
		return errors.New("TICKETS_URL debe ser un origen HTTP sin credenciales, rutas ni query")
	}
	return nil
}
func ConfigFromEnv() (Config, error) {
	c := Config{Port: 4007, JWTSecret: os.Getenv("JWT_SECRET"), InternalAPIKey: os.Getenv("INTERNAL_API_KEY"), TicketsURL: os.Getenv("TICKETS_URL"), HTTPTimeout: 2 * time.Second, OperationTimeout: 6 * time.Second}
	if value, ok := os.LookupEnv("PORT"); ok {
		port, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, errors.New("PORT no válido")
		}
		c.Port = port
	}
	for name, target := range map[string]*time.Duration{"HTTP_TIMEOUT_MS": &c.HTTPTimeout, "OPERATION_TIMEOUT_MS": &c.OperationTimeout} {
		if value, ok := os.LookupEnv(name); ok {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 || n > 6000 {
				return Config{}, errors.New("timeout no válido")
			}
			*target = time.Duration(n) * time.Millisecond
		}
	}
	return c, c.Validate()
}
