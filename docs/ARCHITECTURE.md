# Arquitetura do NVR

## 1. Escopo

O NVR foi projetado para crescer de um único servidor para uma malha de videomonitoramento municipal sem trocar a arquitetura.

Hierarquia lógica:

```
tenant
  └── city
      └── site
          └── edge-node
              └── camera
                  ├── main stream
                  ├── sub stream
                  └── analysis profiles
```

## 2. Componentes

### Control API
Responsável por usuários, RBAC, cidades, sites, câmeras, políticas, plugins, retenção, buscas, auditoria e emissão de comandos.

### Node Agent
Executa em cada servidor de edge. Registra o nó, reporta saúde/recursos, aplica configurações e supervisiona media workers e plugins.

### Media Engine
Responsável por:
- RTSP/ONVIF;
- reconexão automática;
- timestamps;
- remux sem transcodificação quando possível;
- gravação segmentada;
- geração de frames de análise;
- live pipeline;
- buffer pré-evento;
- detecção de falhas do stream.

### Live Gateway
Entrega WebRTC para baixa latência e HLS como fallback. O navegador nunca recebe credenciais da câmera.

### Plugin Runtime
Gerencia ciclo de vida, permissões, recursos, healthcheck e contratos dos plugins.

### Event Bus
NATS/JetStream transporta comandos e eventos duráveis. Raw video não trafega pelo barramento.

### PostgreSQL/PostGIS
Banco de configuração e metadados. Eventos de alto volume serão particionados por data e poderão ter índices geoespaciais.

### Storage Manager
Gerencia segmentos de gravação, snapshots e evidências. Backends:
- local filesystem;
- S3-compatible archive.

Nunca armazenar vídeo bruto em PostgreSQL.

## 3. Caminho do vídeo

```
Camera RTSP
   |
   v
Media Engine
   |-----------------------> Recorder (original codec)
   |
   +--> Decode once
          |
          +--> Live pipeline
          |
          +--> Frame Broker
                 |
                 +--> plate_ocr
                 +--> object_detection
                 +--> future plugins
```

O objetivo é decodificar uma única vez por perfil de análise e reutilizar o frame entre plugins.

## 4. Frame Broker

Para evitar publicar imagens grandes no NATS:

- plugins locais recebem frames por Unix Domain Socket/gRPC streaming;
- payload pode usar JPEG/WebP ou shared memory quando necessário;
- NATS carrega apenas metadados, comandos, resultados e eventos;
- cada plugin declara FPS, resolução e pixel format desejados;
- o broker aplica backpressure e drop policy para analytics em atraso.

Plugins nunca podem bloquear a gravação.

## 5. Gravação

Princípios:
- preservar H.264/H.265 original sempre que possível;
- segmentação curta, independente de processo;
- arquivo temporário + rename atômico após fechamento;
- índice de segmentos no PostgreSQL;
- retenção por câmera/política;
- limpeza por watermark de disco;
- proteção de segmentos marcados como evidência;
- hash SHA-256 para exportações/evidências.

O sistema deve continuar gravando durante indisponibilidade do control plane.

## 6. Edge-first

Para cidades, cada região/site deve ter nós próximos das câmeras.

O control plane recebe:
- estado;
- eventos;
- miniaturas;
- telemetria;
- índices.

Vídeo completo sobe apenas:
- sob demanda;
- por política de evidência;
- replicação de arquivo;
- contingência.

## 7. Alta disponibilidade

### Edge
- gravação é local-first;
- cada câmera possui owner node;
- failover opcional para segundo edge node;
- reconexão com jitter/backoff;
- configuração cacheada localmente.

### Central
- API stateless;
- PostgreSQL com backup/replicação;
- NATS JetStream em cluster na fase HA;
- storage S3 compatível redundante.

## 8. Segurança

- TLS externo e mTLS entre nós.
- OIDC-ready e autenticação local de contingência.
- RBAC por tenant/cidade/site/câmera.
- credenciais das câmeras criptografadas;
- plugins não recebem segredos sem permissão explícita;
- logs de auditoria para login, visualização, exportação, configuração e pesquisa de placas;
- segregação de rede para câmeras;
- tokens curtos para live view;
- rate limit e proteção contra brute force.

## 9. LGPD e governança

OCR de placas e imagens podem constituir dados pessoais dependendo do contexto. A plataforma deve permitir:
- retenção configurável;
- justificativa/propósito por tenant;
- auditoria de consulta;
- mascaramento por perfil;
- exportação autorizada;
- exclusão por política;
- restrição de busca e hotlists.

## 10. Observabilidade

Cada serviço expõe:
- health/readiness;
- métricas;
- logs estruturados;
- traces distribuídos.

Métricas críticas:
- camera_online;
- rtsp_reconnect_total;
- ingest_bitrate;
- dropped_frames;
- recording_gap_seconds;
- storage_free_bytes;
- plugin_latency_ms;
- plugin_queue_depth;
- inference_fps;
- ocr_confidence;
- event_publish_failures.

## 11. Capacidade

Capacidade deve ser calculada por bitrate, não por quantidade nominal de câmeras.

Exemplo: câmera a 4 Mb/s ≈ 43,2 GB/dia.
- 100 câmeras ≈ 4,32 TB/dia.
- 1.000 câmeras ≈ 43,2 TB/dia.
- 1.000 câmeras por 30 dias ≈ 1,30 PB antes de overhead/redundância.

Isso obriga retenção em camadas e análise perto da origem.

## 12. APIs

### REST/OpenAPI
Administração, pesquisa e integrações externas.

### gRPC/Protobuf
Node agent, media engine e plugins.

### NATS subjects
Namespace inicial:
```
nvr.v1.node.*
nvr.v1.camera.*
nvr.v1.event.*
nvr.v1.plugin.*
nvr.v1.command.*
```

O schema de eventos sempre será versionado.

## 13. Formatos de eventos

Todo evento deve ter pelo menos:
- event_id;
- event_type;
- schema_version;
- tenant_id;
- city_id;
- site_id;
- node_id;
- camera_id;
- observed_at;
- received_at;
- plugin;
- confidence;
- snapshot_ref opcional;
- clip_ref opcional;
- attributes.

## 14. Falhas que o desenho precisa tolerar

- câmera desligada;
- RTSP congelado sem socket cair;
- perda de pacotes;
- relógio errado;
- disco cheio;
- GPU indisponível;
- plugin travado;
- banco central offline;
- WAN interrompida;
- reinicialização inesperada;
- segmento de vídeo incompleto;
- fila crescendo;
- versão incompatível de plugin.

Nenhuma dessas falhas deve derrubar todo o NVR.
