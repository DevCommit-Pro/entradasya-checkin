# Checkin — Go

Implementa POST /checkin, GET /checkin/events/{id}/stats y GET /health. Memoria local; tickets es la fuente de verdad del consumo único. Sin DB ni dependencia directa con events.

## Ejecutar

```sh
cp .env.example .env
# Configurar los mismos secretos que auth/tickets.
set -a
source .env
set +a
go run ./cmd/api
```

PORT 4007 por defecto, JWT_SECRET e INTERNAL_API_KEY obligatorios (mínimo 32 bytes), TICKETS_URL obligatorio (origen HTTP sin path/query/credenciales), HTTP_TIMEOUT_MS 2000 (máximo 3000), OPERATION_TIMEOUT_MS 6000 (máximo 6000). Escucha en 127.0.0.1. Go no carga .env automáticamente. No requiere instalaciones adicionales.

## Contrato público

- POST /checkin requiere JWT doorman y body `{code,eventId}`. Código normalizado a mayúsculas y trim de espacios externos. DoormanId/nombre y scanId no se aceptan del frontend: se derivan del JWT y de un identificador aleatorio del servidor.
- 200 CheckinResult para allowed o rejected (invalid, wrong_event, already_used); todos los campos nullable están presentes. already_used incluye hora del primer ingreso.
- 400 datos inválidos, 401 token inválido, 403 rol incorrecto, 404 evento inexistente y 503 si no se puede confirmar la operación. Un error técnico NO se convierte en ingreso permitido ni código inválido.
- GET /checkin/events/{id}/stats requiere portero o organizador dueño. Portero opera sobre eventos publicados; organizador propietario puede consultar también estados históricos. Cliente recibe 403, organizador ajeno 404.
- GET /health informa memoria local y dependencias not_checked; no promete disponibilidad remota.

No hay asignación portero-evento ni ventana horaria adicional de ingreso en esta demo: se exige evento publicado. La web llama las rutas públicas; solo checkin usa X-Internal-Key para comunicarse con tickets.

## Consumo y recuperación

Cada POST crea un scanId nuevo. Hasta dos intentos internos conservan ese mismo scanId dentro del presupuesto de tiempo. Tickets consume atómicamente; dos escaneos concurrentes no permiten dos ingresos. Se valida el recibo antes de mostrar allowed y registrar auditoría local.

Si el resultado sigue siendo incierto, responder 503. **No se disparan consumos en segundo plano después de ese error.** Las estadísticas leen los usos confirmados de tickets y reconstruyen auditoría, incluso si tickets consumió pero checkin no recibió respuesta o falló el registro local. Un nuevo escaneo físico crea otro intento y puede mostrar already_used con la hora original; nunca se desbloquea una entrada automáticamente.

Estadísticas: usos confirmados primero, conteo de emitidas después; entered representa ese snapshot de usos, recent contiene los diez más recientes. No es una transacción distribuida: ingresos nuevos entre consultas pueden aparecer en el siguiente refresco. Datos incoherentes (duplicados, issued menor que usos, recibos contradictorios) producen 503, no contadores inventados. La auditoría se importa sin escrituras parciales y no determina por sí sola los contadores tras reinicios de tickets. Reiniciar servicios coordinadamente para restablecer la demo.

Respuestas privadas Cache-Control:no-store. Logs JSON sin JWT, claves, códigos QR, nombres ni cuerpos de solicitud. Despliegue, frontend, PDF/QR y correo real quedan fuera de esta entrega.

## Pruebas

```sh
go test -race ./...
go vet ./...
```

Pruebas de servicio/handlers/cliente HTTP en memoria, concurrencia y pérdida de respuestas. Incluye una prueba de interoperabilidad (`auth_interop_test.go`) que valida tokens HS256 con el formato y claims emitidos por NestJS (`@nestjs/authentication`) sin depender de carpetas externas. No abren sockets ni equivalen al recorrido de procesos/frontend.
