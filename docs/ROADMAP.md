# Roadmap

## Fase 0 — fundação
- [x] ADRs e contratos.
- [x] Protobuf de node/plugin/event.
- [x] schema de plugin.
- [x] estrutura do monorepo.
- [x] CI.
- [x] políticas de versionamento.

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
- [x] snapshot HTTP/Digest;
- [x] proteção de segmento/evidência;
- [ ] exportação de clip com corte exato;
- [ ] usuários/RBAC completo.

## Fase 2 — plugin runtime + Plate OCR
- [x] frame broker codificado não bloqueante;
- [x] transporte de frame JPEG autorizado para plugins;
- [x] token exclusivo de plugin;
- [x] ingestão idempotente de eventos;
- [x] armazenamento de evidência com SHA-256;
- [x] pesquisa de placa exata/parcial por API;
- [x] plate_ocr end-to-end com provider clássico offline;
- [x] deduplicação multi-frame;
- [x] spool/replay durante indisponibilidade;
- [x] métricas de runtime do Plate OCR;
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

Antes de produção, medir streams por nó, bitrate, CPU/GPU, RAM, IOPS, latência, gaps de gravação, frame drop e comportamento com plugin degradado/parado.

Nenhuma meta de câmeras por servidor deve ser aceita sem bitrate, codec, FPS, resolução, retenção e hardware definidos.
