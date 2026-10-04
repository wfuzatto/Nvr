# ADR 0001 — Stack de plataforma

Status: Accepted

## Decisão

### Sistema operacional
Ubuntu Server 26.04 LTS será o baseline de produção. Ubuntu 24.04 LTS poderá ser suportado para hardware/SDKs que ainda dependam dele.

### Containers
Usaremos Docker/OCI. Docker Compose é o orquestrador inicial por edge node. Não começaremos com Kubernetes.

Motivos:
- reproducibilidade;
- rollback;
- isolamento de plugins;
- CPU/RAM/GPU limits;
- implantação uniforme;
- caminho futuro para k3s/Kubernetes sem reempacotar serviços.

Drivers, filesystem e montagem dos discos continuam no host.

### Go
Usado no control plane e node agent por sua concorrência, baixo consumo, binários simples e boa adequação a serviços de rede.

### Rust
Usado no media engine para integração segura e eficiente com pipelines de mídia e GStreamer.

### Python
Reservado principalmente a plugins de ML/CV, onde o ecossistema de modelos é superior. Python não fica no caminho crítico de gravação.

### TypeScript/React
Frontend web.

### GStreamer
Pipeline principal de ingest/decode/branching/analytics.

### FFmpeg
Ferramenta complementar para probing, remux/exportação, testes e compatibilidade.

### NATS JetStream
Barramento de eventos/comandos duráveis e desacoplamento de plugins/serviços.

### PostgreSQL/PostGIS
Fonte canônica de configuração/metadados e recursos geográficos.

### Storage
Filesystem local para gravação quente; API S3-compatible para archive/evidências.

## Consequências

A plataforma terá múltiplas linguagens, mas cada uma terá responsabilidade clara. Contratos Protobuf e eventos versionados impedem acoplamento de implementação.

Não serão colocados:
- frames grandes no NATS;
- vídeo no PostgreSQL;
- credenciais de câmera nos plugins por padrão;
- dependência de Kubernetes na primeira versão.
