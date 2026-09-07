# SMMiner

[English](README.md) | [Português (Brasil)](README.pt-BR.md)

O SMMiner é um cliente de mineração de Bitcoin escrito em Go, voltado para dispositivos **AonMiner ZX1 USB ASIC** (baseados em BM1368). Ele usa o protocolo Stratum V1 para se conectar à pool de mineração.

## Hardware e Dependências
- AonMiners ZX1 USB ASIC (FTDI 0403:6015)
- [usbreset](https://github.com/jkulesza/usbreset) (Apenas Linux, para reset automático do hub USB. Está incluído no usbutils do Ubuntu APT)
- libusb
- pkgconfig
- gcc
- Zadig (Windows, para drivers WinUSB ou libusbK)

## Início Rápido

```bash
# Compilar (Linux ou MacOS)
go build .

# Compilar (Windows no Msys2-Mingw)
CGO_ENABLED=1 go build -ldflags '-linkmode external -extldflags "-static"' .

# Executar (mineração solo no ckpool)
./smminer -o "stratum+tcp://solo.ckpool.org:3333" -u "SEU_ENDERECO_BTC" -p "x"
```

Para um exemplo completo com suporte a reset de hub USB, veja [`automine.sh`](automine.sh).

## Configuração do udev no Linux

Execute uma vez para permitir acesso USB sem root:

```bash
sudo bash miner-rules.sh
```

Depois desconecte e reconecte seus dispositivos de mineração.

## Flags da CLI

| Flag | Padrão | Descrição |
|------|---------|-------------|
| `-o` | *(obrigatório)* | URL do pool (com ou sem `stratum+tcp://`) |
| `-u` | *(obrigatório)* | Nome do worker do pool / endereço BTC |
| `-p` | `x` | Senha do pool |
| `--aon-frequency` | `150` | Frequência do ASIC em MHz |
| `--aon-job-timer` | `20` | Temporização de jobs do ASIC em milissegundos |
| `--suggest-diff` | `500` | Dificuldade sugerida ao pool |
| `--aon-baudrate` | `1` | Taxa de transmissão: `1` = 1M, `2` = 1.5M |
| `--aon-usb-hub` | `1234:0001` | VID:PID do hub USB para reset forçado |
| `--version` | `false` | Exibe a versão e sai |

## Avisos de Segurança

- **Sempre pare com `CTRL+C`** — nunca feche a janela do terminal à força.
- Se o minerador for fechado à força, o ASIC pode travar. Desconecte e reconecte o cabo USB para recuperar.
- O minerador pode atrasar o encerramento durante operações críticas — aguarde terminar.

## Estrutura do Projeto

```
smminer/
├── smminer.go          # Ponto de entrada, parsing de CLI, inicialização dos loops
├── stratum.go          # Protocolo Stratum (conexão com o pool, jobs e envio de resultados)
├── miner.go            # Controladores do minerador (AonMiner, CPUMiner -> MinerReceiver)
├── drivers/
│   ├── aonminer.go     # Driver dos ASICs AonMiner
│   ├── cpu.go          # Minerador de CPU (para testes, ative via constante CPU_MODE em smminer.go)
│   └── components/     # Definições dos chips BM13xx
│       ├── bm13xx.go   # Interface comum dos chips
│       └── bm1368.go   # Constantes do chip BM1368
├── util/
│   ├── general.go      # Utilitários hex/bit, formatadores, validador SHA256d
│   ├── sha256.go       # Implementação de SHA256 e cálculo de midstate
│   ├── cli.go          # Logging e auxiliares de CLI
│   └── usb.go          # Comunicação serial USB (FTDI)
├── automine.sh         # Exemplo de script de mineração
├── miner-rules.sh      # Configuração das regras udev no Linux
├── go.mod
├── go.sum
└── LICENSE
```

Requer Go 1.24+.

## Referências

- Protocolo Stratum V1: https://reference.cash
- Núcleo de mineração SHA-256 e validações: https://learnmeabitcoin.com
- Registros de chip: [cgminer](https://github.com/ckolivas/cgminer) e [esp-miner](https://github.com/bitaxeorg/ESP-Miner) 

## Licença

MIT — veja [LICENSE](LICENSE).
