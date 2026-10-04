# Roadmap

## Fase 0 — fundação
- ADRs e contratos.
- Protobuf de node/plugin/event.
- schema de plugin.
- estrutura do monorepo.
- CI.
- compose de desenvolvimento.
- políticas de versionamento.

## Fase 1 — NVR single-node
- [x] cadastro de câmera;
- [x] ONVIF discovery, profiles, provisioning e PTZ;
- [x] RTSP ingest persistente;
- [x] health monitor / watchdog;
- [x] gravação H.264/H.265 segmentada;
- [x] timeline;
- [x] playback HLS/VOD sem reencode;
- [x] live HLS sem reencode;
- [x] WebRTC H.264 de baixa latência com fallback HLS;
- [x] retenção;
- [x] snapshot HTTP/Digest quando a câmera fornece endpoint;
- [x] proteção de segmento/evidência;
- [ ] exportação de clip com corte exato;
- [ ] usuários/RBAC completo.

Critério: operar 24/7 sem vazamento de recursos e recuperar automaticamente streams interrompidos.

## Fase 2 — plugin runtime + Plate OCR
- [x] frame broker codificado não bloqueante;
- [ ] decoder/frame broker em pixel format;
- [ ] instalação/enable/disable de plugins.
- resource limits.
- plate_ocr end-to-end.
- busca por placa.
- snapshot/clip do evento.
- deduplicação multi-frame.
- hotlist com alerta.
- métricas de precisão/latência.

## Fase 3 — multi-node
- node enrollment.
- control plane central.
- distribuição de câmeras.
- cache local de configuração.
- operação offline.
- reconciliação.
- mapa de câmeras.
- health dashboard municipal.

## Fase 4 — HA e escala
- PostgreSQL HA.
- NATS cluster.
- storage archive.
- failover de edge.
- backup/restore automatizado.
- disaster recovery.
- testes de centenas/milhares de streams simulados.

## Fase 5 — segurança/evidência
- OIDC/MFA.
- mTLS.
- auditoria completa.
- evidência com hash e cadeia de custódia.
- políticas LGPD.
- segregação multi-tenant.

## Fase 6 — ecossistema de plugins
Exemplos:
- people_detection;
- vehicle_classification;
- face_blur;
- intrusion;
- abandoned_object;
- smoke_fire;
- crowd_density;
- wrong_way;
- speed_estimation;
- webhook;
- notifications;
- access_control.

## Gates de performance

Antes de produção, medir:
- número de streams por nó;
- bitrate total;
- CPU por stream;
- GPU decode;
- GPU inference;
- RAM por camera;
- IOPS;
- disk write throughput;
- reconnection time;
- event latency;
- recording gaps;
- frame drop;
- capacidade com plugin degradado/parado.

Nenhuma meta de “X câmeras por servidor” deve ser aceita sem bitrate, codec, FPS, resolução, retenção e hardware definidos.
