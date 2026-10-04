# Pacote offline

Esta pasta transforma o checkout do repositório em um pacote instalável **sem Internet**.

## Servidor Ubuntu / Debian com systemd

```bash
sudo sh offline/install.sh
```

O instalador:

1. detecta amd64/arm64;
2. usa somente o binário já presente em `offline/bin`;
3. valida SHA-256;
4. cria o usuário de serviço;
5. instala em `/opt/nvr`;
6. cria `/var/lib/nvr`;
7. instala e inicia o serviço systemd.

Ele não executa `apt`, `curl`, `wget`, `docker pull`, `go get` ou qualquer download.

## Execução portátil

```bash
sh offline/run-portable.sh
```

## Binários

Os executáveis são produzidos pelo CI com `CGO_ENABLED=0` e gravados de volta neste repositório:

- `offline/bin/linux-amd64/nvr`
- `offline/bin/linux-arm64/nvr`

Cada binário acompanha um arquivo `.sha256`.

O CI só aceita o core se `go test ./...` funcionar com `GOPROXY=off` e `GOSUMDB=off`.
