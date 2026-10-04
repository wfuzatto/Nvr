# Arquitetura de Plugins

## Objetivo

O NVR deve aceitar recursos novos sem recompilar o núcleo e sem permitir que um plugin defeituoso derrube gravação ou live view.

## Tipos de plugin

1. **Frame Processor** — recebe frames e produz eventos (OCR, pessoas, fumaça, objetos).
2. **Event Processor** — consome eventos e enriquece/correlaciona dados.
3. **Action Plugin** — executa ações externas (alarme, webhook, barreira, mensagem).
4. **Source Adapter** — integração opcional com fontes/VMS externos.
5. **Exporter** — envia eventos/evidências para sistemas terceiros.

## Manifesto

Todo plugin possui `plugin.yaml` com:
- id e versão;
- versão do protocolo;
- capabilities;
- configuração JSON Schema;
- recursos mínimos;
- necessidade de GPU;
- frame profiles;
- eventos consumidos/publicados;
- permissões;
- healthcheck.

## Isolamento

Por padrão cada plugin roda em container separado com:
- CPU/memory limits;
- filesystem read-only quando possível;
- volume próprio;
- sem acesso ao socket Docker;
- rede mínima necessária;
- GPU explicitamente atribuída;
- restart policy;
- healthcheck.

## Frames

O plugin não abre RTSP diretamente.

Fluxo:
```
camera -> media-engine -> frame-broker -> plugin
```

Contrato de assinatura inclui:
- camera IDs;
- max_fps;
- max_width/max_height;
- pixel_format;
- jpeg_quality ou raw/shared-memory;
- zone/ROI opcional.

Se o plugin não acompanhar a taxa, frames de analytics podem ser descartados. A gravação nunca espera pelo plugin.

## Eventos

Eventos e comandos usam NATS/JetStream. O plugin precisa ser idempotente porque mensagens podem ser reentregues.

Cada evento tem `event_id` global e `dedupe_key` opcional.

## Compatibilidade

O protocolo segue major version:
- `nvr.plugin.v1`
- mudanças compatíveis não quebram plugins v1;
- breaking changes criam v2.

O NVR deve suportar pelo menos uma janela de migração entre versões.

## Permissões sugeridas

```
frames:read
events:read
events:publish
snapshots:write
clips:request
config:read
network:egress
gpu:use
camera:ptz
action:execute
```

Plugins não recebem `camera:credentials` por padrão.

## Lifecycle

Estados:
```
installed -> configured -> starting -> healthy
                           -> degraded
                           -> failed
                           -> stopped
```

Heartbeats e métricas são obrigatórios.

## SDK

O NVR manterá:
- Protobuf canônico;
- exemplos;
- helper libraries;
- simulador de frames;
- test harness;
- validator de manifesto.

A primeira implementação de referência é `Plate_ocr`.
