# Plugin Runtime HTTP v1

Implementação local do contrato `nvr.plugin.v1` para plugins externos sem exposição de credenciais de câmera.

## Segurança

O NVR cria `NVR_DATA_DIR/plugin.token` com permissão restrita. O token é separado do token administrativo e das sessões RBAC. Plugins não recebem RTSP, usuário nem senha de câmera.

## Endpoints de plugin

### GET /api/v1/plugin/v1/cameras

Retorna metadados mínimos e `snapshot_available`.

### GET /api/v1/plugin/v1/cameras/{id}/frame

Busca o snapshot usando as credenciais mantidas no core e entrega JPEG normalizado. PNG é convertido para JPEG. O header `X-NVR-Observed-At` informa o instante de captura.

### POST /api/v1/plugin/v1/evidence

Recebe JPEG de até 12 MiB. `X-Event-ID` é obrigatório. O arquivo é idempotente por event ID; reenvio com bytes diferentes retorna conflito. A resposta inclui `snapshot_ref` e SHA-256.

### POST /api/v1/plugin/v1/events

Recebe `nvr.event.plate.detected.v1`. `event_id` é idempotente e a evidência referenciada precisa existir.

## API administrativa

Requer permissão RBAC `evidence`.

### GET /api/v1/events/plates

Filtros: `plate`, `camera_id`, `from`, `to`, `limit`.

### GET /api/v1/events/{id}/evidence

Entrega a evidência JPEG do evento.

## Evolução

O HTTP local é o transporte operacional v1. O envelope permanece compatível com evolução futura para gRPC/NATS/JetStream e frame broker decodificado.
