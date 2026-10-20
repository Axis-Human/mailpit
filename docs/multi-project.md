# Modo multi-proyecto

Por defecto Mailpit tiene un único buzón sin login. Con `--multi-project` (o `MP_MULTI_PROJECT=true`) pasa a funcionar con:

- **Proyectos**: cada uno es un buzón aislado (mensajes, tags, búsquedas, estadísticas y websocket) con su propia **API key**.
- **Usuarios**: entran con email y contraseña, y cada usuario ve solo los proyectos que tiene asignados.
- **Superadmin**: crea y administra proyectos y usuarios, y accede a todos los proyectos.
- **Deshabilitar un proyecto**: la API key deja de responder (SMTP y API) y se rechazan los mails entrantes. Los mensajes ya recibidos se conservan y los usuarios del proyecto pueden seguir consultándolos desde la web.

Sin el flag todo funciona exactamente igual que antes.

## Cómo se usa

### Superadmin

Se crea al arrancar con `MP_SUPERADMIN_EMAIL` y `MP_SUPERADMIN_PASSWORD` (mínimo 8 caracteres). La contraseña solo se usa si la cuenta no existe todavía; después se cambia desde la web (🔑). Si se pierde, arrancar una vez con `MP_SUPERADMIN_RESET_PASSWORD=true` para volver a fijarla con el valor de `MP_SUPERADMIN_PASSWORD`.

Desde la web, el botón ⚙ del menú lateral abre **Administración**:

| Pestaña | Acciones |
| --- | --- |
| Projects | Crear, editar nombre/slug, **deshabilitar / habilitar**, generar una nueva API key, eliminar (borra también sus mensajes) |
| Users | Crear, editar, asignar proyectos, deshabilitar, eliminar, marcar como superadmin |

La API key completa se muestra **una sola vez** al crear el proyecto o al regenerarla; en la base de datos solo se guarda su hash. Al regenerarla la anterior deja de funcionar al instante.

### Aplicaciones que envían mails

**SMTP**: el *password* es la API key del proyecto; el usuario puede ser cualquiera (se sugiere el slug, y queda registrado en el mensaje).

```
host:     <host del servidor>
puerto:   1025
usuario:  acme
password: mpk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

**API HTTP**: la misma key en cualquiera de estas cabeceras. Una key solo accede a los mensajes de su proyecto.

```
Authorization: Bearer mpk_...
X-API-Key: mpk_...
Authorization: Basic base64(<cualquiera>:mpk_...)
```

```sh
curl -H "X-API-Key: $KEY" https://mail.example.com/api/v1/messages
curl -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"From":{"Email":"a@b.test"},"To":[{"Email":"c@d.test"}],"Subject":"Hola","Text":"Hola"}' \
  https://mail.example.com/api/v1/send
```

Respuestas con un proyecto deshabilitado: SMTP `535 5.7.8 This project has been disabled` (o `550 5.7.1` si la conexión ya estaba autenticada) y HTTP `403`.

### Variables de entorno

| Variable | Descripción |
| --- | --- |
| `MP_MULTI_PROJECT` | Activa el modo multi-proyecto |
| `MP_SUPERADMIN_EMAIL` / `MP_SUPERADMIN_PASSWORD` | Superadmin inicial |
| `MP_SUPERADMIN_RESET_PASSWORD` | `true` para reiniciar la contraseña del superadmin en el arranque |
| `MP_SESSION_HOURS` | Horas de validez de un login (por defecto 168) |
| `MP_DATABASE` | Ruta de la base SQLite, **obligatoria en producción** para no perder datos |
| `MP_MAX_MESSAGES` | Mensajes que conserva **cada proyecto** (por defecto 500) |
| `MP_SMTP_AUTH_ALLOW_INSECURE` | Permite autenticar sin TLS |
| `MP_SMTP_TLS_CERT` / `MP_SMTP_TLS_KEY` | STARTTLS para SMTP (rutas, o `sans:<host>` para un certificado autofirmado) |

Incompatibilidades (Mailpit no arranca): `--ui-auth-file`, `--send-api-auth-file`, `--smtp-auth-file`, `--smtp-auth-accept-any` y `--send-api-auth-accept-any`, porque la autenticación la resuelven los usuarios y las API keys. Además, en este modo el servidor POP3 y las métricas Prometheus integradas están desactivados (usar `--enable-prometheus <ip:puerto>` para un servidor separado, que expone los totales de todos los proyectos).

## Deploy con Docker Compose

El repositorio incluye un `docker-compose.yaml` que construye la imagen desde el `Dockerfile`.

1. Definir las variables de entorno (en un `.env` junto al compose, o en la plataforma de despliegue). Como mínimo:
   - `MP_SUPERADMIN_EMAIL`
   - `MP_SUPERADMIN_PASSWORD`

   Opcionales: `SMTP_PORT` (puerto publicado, 1025 por defecto), `MP_MAX_MESSAGES`, `MP_MAX_AGE`, `MP_SMTP_*`, `MP_SESSION_HOURS`.
2. `docker compose up -d --build`.
3. Publicar la web y la API (puerto `8025` del contenedor) detrás de un proxy inverso con HTTPS que reenvíe `X-Forwarded-Proto`, con lo que la cookie de sesión se marca `Secure` automáticamente.
4. Los datos viven en el volumen `mailpit-data` (`/data/mailpit.db`), que persiste entre despliegues.
5. Abrir el dominio, entrar con el superadmin y crear el primer proyecto.

Notas:

- **Puerto SMTP**: SMTP no es HTTP, no pasa por el proxy inverso; el compose publica `1025` directamente en el servidor. Hay que abrirlo en el firewall y las aplicaciones usan `host-del-servidor:1025`.
- **TLS en SMTP**: por defecto el compose permite autenticar sin cifrado (`MP_SMTP_AUTH_ALLOW_INSECURE=true`), lo que implica que la API key viaja en claro. Es aceptable en una red privada; si el puerto está expuesto a Internet, poner `MP_SMTP_AUTH_ALLOW_INSECURE=false` y configurar `MP_SMTP_TLS_CERT` / `MP_SMTP_TLS_KEY` (STARTTLS pasa a ser obligatorio antes de autenticar).
- **Una sola réplica**: Mailpit usa SQLite local, no escalar a más de una instancia.
- **Sin compose**: con la imagen del `Dockerfile` basta publicar `8025` y `1025`, montar un volumen en `/data` y pasar las mismas variables, añadiendo `MP_MULTI_PROJECT=true` y `MP_DATABASE=/data/mailpit.db`.

## Notas de diseño

- Cada mensaje y cada tag lleva un `ProjectID`; todas las consultas (listado, búsqueda, borrado, tags, estadísticas, adjuntos, previews) filtran por proyecto, y los websockets solo entregan eventos del proyecto del cliente.
- Un usuario con sesión elige el proyecto con la cabecera `X-Mailpit-Project` (slug o ID) o el parámetro `?project=`; los enlaces a un mensaje concreto (adjuntos, raw, preview) resuelven el proyecto desde el propio mensaje y comprueban el acceso. Una API key está atada a su proyecto.
- Sesiones: cookie `HttpOnly`, `SameSite=Strict`, con el token guardado hasheado. Cambiar la contraseña o deshabilitar al usuario cierra todas sus sesiones. El login limita los intentos fallidos por email y por IP.
- Las cuentas se guardan con bcrypt; las API keys y tokens de sesión con SHA-256 (son aleatorios de alta entropía).
- Los datos existentes de un Mailpit previo (`ProjectID` vacío) no pertenecen a ningún proyecto en este modo, por lo que no son visibles.
