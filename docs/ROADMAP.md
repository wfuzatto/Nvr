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
- [x] mosaico live 1/4/9/16;
- [x] timeline visual por intervalo;
- [x] dashboard de saúde + métricas Prometheus;
- [x] auditoria encadeada e verificação de integridade;
- [x] retenção;
- [x] snapshot HTTP/Digest quando a câmera fornece endpoint;
- [x] proteção de segmento/evidência;
- [ ] exportação de clip com corte exato;
- [ ] usuários/RBAC completo.

Critério: operar 24/7 sem vazamento de recursos e recuperar automaticamente streams interrompidos.

## Fase 2 — plugin runtime + Plate OCR
- [x] frame broker codificado não bloqueante;
- [x] transporte JPEG normalizado e autenticado para plugins;
- [x] token exclusivo de plugin sem exposição de credenciais RTSP;
- [x] ingestão idempotente de eventos;
- [x] armazenamento de evidência JPEG com SHA-256;
- [x] pesquisa exata/parcial de placas por API e RBAC;
- [x] plate_ocr end-to-end com provider clássico offline;
- [x] ROI/faixa/direção por câmera;
- [x] deduplicação e votação multi-frame;
- [x] spool/replay quando o NVR está indisponível;
- [x] healthcheck e métricas do Plate OCR;
- [ ] decoder contínuo/frame broker NV12 para analytics de FPS alto;
- [ ] provider neural validado para cenários difíceis;
- [ ] hotlist com alerta;
- [ ] clip exato associado ao evento.

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
