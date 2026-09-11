# urlcheck

CLI en Go para responder: **¿qué chats y APIs de agentes puedo usar desde esta computadora?**

Lee URLs desde un archivo plano y comprueba si el host responde, si el DNS las hunde o si hay 403/451.

## Instalar

```bash
curl -fsSL https://raw.githubusercontent.com/pablontiv/urlcheck/main/install.sh | sh
```

Instala el binario en `~/.local/bin` y la lista en `~/.config/urlcheck/services.txt`.

```bash
# otro destino
curl -fsSL https://raw.githubusercontent.com/pablontiv/urlcheck/main/install.sh | PREFIX=/usr/local/bin sh

# versión concreta (cuando existan releases)
curl -fsSL https://raw.githubusercontent.com/pablontiv/urlcheck/main/install.sh | URLCHECK_VERSION=v0.1.0 sh
```

Hoy no hay tag de Release: el script clona y compila con `go` si hace falta.

```bash
urlcheck -list ~/.config/urlcheck/services.txt
```

## Formato

```
# comentarios
ChatGPT                 https://chatgpt.com
OpenAI API              https://api.openai.com/v1                 api
```

Tercera columna opcional: `web` (sitio) o `api` (base URL que usan agentes / SDKs).
Si omites el tipo, se infiere por el nombre o el host (`api.`, `/v1`, etc.).

También vale `Nombre | URL api`.

## Uso

```bash
go run .                         # usa services.txt
go run . -list mis_urls.txt
go run . -timeout 5s -workers 20
go run . -json
go run . -method GET
```

```bash
go build -o urlcheck .
./urlcheck
```

## Binarios Linux / macOS

Pipeline: `.github/workflows/binaries.yml`

| OS | Arch |
|----|------|
| linux | amd64, arm64 |
| darwin (macOS) | amd64, arm64 |

El build corre **solo al mergear a `main`**. Un tag `v*` publica un Release.

```bash
git tag v0.1.0 && git push origin v0.1.0
```

## Estados

| Estado | Sitio (`web`) | API (`api`) |
|--------|---------------|-------------|
| **OK** | 2xx/3xx o 401 | Cualquier HTTP excepto 451/5xx: el endpoint es alcanzable (aunque pida key) |
| **BLOCKED** | sinkhole DNS, 451, 403 | sinkhole DNS o 451 |
| **DOWN** | timeout, refused, 5xx, DNS roto | timeout, refused, 5xx, DNS roto |
| **ERROR** | TLS u otro | TLS u otro |

Un 403 en un *sitio* suele ser WAF, no siempre censura de ISP.
Un 401/403/404 en una *API* casi siempre significa “el host vive y pide autenticación”: el agente **puede** apuntar ahí.

La herramienta no valida API keys. Solo alcanzabilidad de red.
