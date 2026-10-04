# ONVIF v1

## Objetivo

A camada ONVIF permite descobrir, provisionar e controlar câmeras sem depender de bibliotecas externas ou Internet.

## Discovery

Endpoint:

`GET /api/v1/onvif/discover?timeout_ms=3000`

O NVR envia WS-Discovery Probe para `239.255.255.250:3702` por todas as interfaces IPv4 ativas com multicast.

O retorno inclui:

- EndpointReference;
- XAddrs;
- Scopes;
- Types;
- IP/porta de origem.

## Inspect

`POST /api/v1/onvif/inspect`

Entrada:

```json
{
  "endpoint": "http://10.0.0.10/onvif/device_service",
  "username": "admin",
  "password": "..."
}
```

O NVR consulta:

- GetDeviceInformation;
- GetServices;
- GetCapabilities como fallback;
- GetProfiles em Media2 quando disponível;
- Media1 como fallback.

## Autenticação

São suportados:

- WS-Security UsernameToken PasswordDigest;
- HTTP Digest MD5;
- HTTP Digest MD5-sess;
- HTTP Basic como fallback.

Senhas não são retornadas pela API.

## Provisionamento

`POST /api/v1/cameras/from-onvif`

O usuário escolhe um profile. O NVR executa:

1. GetProfiles;
2. GetStreamUri;
3. GetSnapshotUri quando suportado;
4. injeta as credenciais localmente nas URIs quando o dispositivo não as retorna;
5. criptografa RTSP, snapshot e endpoint ONVIF;
6. inicia automaticamente o worker de gravação.

O profile token e a versão Media ficam persistidos para sincronização posterior.

## Sync

`POST /api/v1/cameras/{id}/onvif/sync`

Atualiza stream URI, snapshot URI e capacidade PTZ sem recriar a câmera.

## PTZ

Endpoints:

- `GET /api/v1/cameras/{id}/ptz/status`
- `POST /api/v1/cameras/{id}/ptz/move`
- `POST /api/v1/cameras/{id}/ptz/stop`

Move usa valores normalizados entre -1 e 1 para pan, tilt e zoom.

## Compatibilidade

A implementação prioriza Profile S/T e câmeras que expõem Media ou Media2. Dispositivos antigos que falham em GetServices usam GetCapabilities como fallback.

Descoberta ONVIF é tráfego LAN/multicast e não é dependência de Internet.
