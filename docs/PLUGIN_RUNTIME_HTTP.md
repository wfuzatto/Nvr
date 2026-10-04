# Plugin Runtime HTTP v1

Esta é a implementação local inicial do contrato `nvr.plugin.v1`. Ela usa HTTP autenticado para permitir plugins externos sem expor credenciais de câmera.

## Autenticação

O NVR cria `NVR_DATA_DIR/plugin.token` com permissão restrita. Plugins locais enviam o bearer token no header `Authorization`.

O token de plugin é diferente do token administrativo.

## Endpoints do plugin

### GET /api/v1/plugin/v1/cameras

Retorna somente metadados mínimos e `snapshot_available`. Não retorna RTSP, usuário ou senha.

### GET /api/v1/plugin/v1/cameras/{id}/frame

Retorna imagem autorizada pelo NVR. Nesta versão o NVR usa o endpoint de snapshot da câmera e mantém as credenciais no core.

Headers úteis:

- `Content-Type`
- `X-NVR-Observed-At`

### POST /api/v1/plugin/v1/evidence

Body JPEG, máximo 12 MiB.

Header obrigatório:

`X-Event-ID`

Resposta inclui `snapshot_ref` e SHA-256.

### POST /api/v1/plugin/v1/events

Aceita o envelope JSON de evento. `event_id` é idempotente: reenvio do mesmo evento retorna sucesso sem duplicar o registro.

## API administrativa

### GET /api/v1/events/plates

Filtros:

- `plate`: parcial ou exata;
- `camera_id`;
- `from` / `to` em RFC3339;
- `limit` 1..5000.

### GET /api/v1/events/{id}/evidence

Retorna a evidência JPEG protegida por autenticação administrativa.

## Escala

O transporte HTTP local é o contrato operacional da versão 1. O envelope permanece compatível com a evolução para gRPC/NATS/JetStream em implantação multi-node.
