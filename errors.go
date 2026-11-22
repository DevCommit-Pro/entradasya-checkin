package checkin

import "time"

type APIError struct {
	Status        int
	Code, Message string
}

func (e *APIError) Error() string { return e.Code }
func unauthorized() *APIError {
	return &APIError{401, "unauthorized", "El acceso requiere credenciales válidas."}
}
func forbidden() *APIError { return &APIError{403, "forbidden", "Tu rol no permite esta operación."} }
func invalid() *APIError {
	return &APIError{400, "invalid_request", "Indica un evento y código válidos."}
}
func missing() *APIError { return &APIError{404, "not_found", "No encontramos el evento."} }
func unavailable() *APIError {
	return &APIError{503, "service_unavailable", "No pudimos confirmar el ingreso. Consulta el estado antes de volver a intentar."}
}

type UpstreamError struct {
	Status int
	Code   string
	UsedAt *time.Time
}

func (e *UpstreamError) Error() string { return "dependencia no disponible o respuesta no válida" }
