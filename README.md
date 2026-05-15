# database_scan

[![build](https://github.com/RuoJi6/database_scan/actions/workflows/build.yml/badge.svg)](https://github.com/RuoJi6/database_scan/actions/workflows/build.yml)

`database_scan` 是一个 Go 编写的数据库敏感信息检索 CLI 工具，用于检查开发数据库中是否存在手机号、身份证、地址、账号、密码、邮箱、银行卡、token/secret 等敏感信息。默认终端表格输出。

## 支持能力

- 数据库：MySQL/MariaDB/TiDB、MSSQL、PostgreSQL、Oracle，以及多种 MySQL/PostgreSQL 协议兼容国产数据库
- 代理：直连、SOCKS5、HTTP CONNECT
- 认证：命令行密码或隐藏交互输入
- 输出：连接信息、按表分组的敏感字段、存在行数和类似 SQL 查询结果的整行真实样例值
- 检索模式：
  - `field-content`：根据表/字段名定位敏感字段，再检索字段内容
  - `field-name`：只检索敏感表名/字段名
  - `content`：扫描字段内容
  - `all`：执行全部模式
- 敏感级别：
  - `high`：身份证、密码/密钥、银行卡
  - `medium`：手机号、邮箱
  - `low`：地址、用户名/账号
  - `all`：全部级别，默认值

## 构建

```bash
go build -o database_scan ./cmd/database_scan
```

## 使用示例

```bash
./database_scan --type mysql --host 127.0.0.1 --port 3306 --user root --password pass
```

也可以把地址和端口写在一起，`--host 192.0.2.10:1433` 或直接把目标作为位置参数：

```bash
./database_scan --type mssql 192.0.2.10:1433 --user sa --password pass
```

如果不希望数据库密码出现在 shell 历史记录中，可以省略 `--password`，程序会提示隐藏输入：

```bash
./database_scan --type mysql --host 127.0.0.1 --port 3306 --user root
```

```bash
./database_scan --type mssql --host 192.0.2.10 --user sa --password pass --proxy socks5://proxy_user:proxy_pass@127.0.0.1:1080 --mode all
```

```bash
./database_scan --type postgres --host 198.51.100.10 --user dev --password pass --mode content --limit 15
```

只检索最高敏级别：

```bash
./database_scan --type mysql --host 127.0.0.1 --user root --password pass --level high
```

```bash
./database_scan --type oceanbase --host 198.51.100.20 --user dev --password pass --database appdb --output result.xlsx
```

```bash
./database_scan --type opengauss --host 198.51.100.30 --user dev --password pass --database appdb --table public.users
```

Oracle 的 `--database` 表示 service name：

```bash
./database_scan --type oracle --host 198.51.100.40 --user system --password pass --database ORCL
```

```bash
./database_scan --type mssql --host 192.0.2.10 --user sa --password pass --database appdb
```

```bash
./database_scan --type mssql --host 192.0.2.10 --user sa --password pass --database appdb --table dbo.Users
```

```bash
./database_scan --type mysql --host 127.0.0.1 --user root --password pass --sql "select user, host from mysql.user"
```

## 参数

- `--type`：数据库类型，见下方支持列表
- `--host` / `--port`：目标地址和端口，端口不填时使用默认端口；也支持 `--host host:port` 或位置参数 `host:port`
- `--user` / `--password`：账号密码；密码不填时交互输入
- `--database`：指定要扫描的单个数据库；不指定时列举并扫描全部可访问数据库
- `--table`：只扫描指定数据库中的某一张表，需要同时指定 `--database`；支持 `Users` 或 `dbo.Users`
- `--proxy socks5://...|http://...`：代理地址
- `--mode field-content|field-name|content|all`：检索模式，默认 `field-content`
- `--level all|high|medium|low`：按敏感级别检索，默认 `all`；`high` 只检索身份证、密码/密钥、银行卡等最高敏信息
- `--limit`：每张命中表最多展示整行样例数量，默认 15
- `--include-system`：包含系统库
- `--mask`：样例值脱敏显示
- `--no-color`：关闭敏感字段和值的颜色标记，适合复制到报告或重定向到文件
- `--no-banner`：关闭启动随机颜文字 banner
- `--no-progress`：关闭运行状态/扫描进度输出
- `--output result.xlsx`：将扫描结果写入 Excel 文件；每个命中表一个 Sheet，上方是敏感字段清单，下方是整行样例数据，敏感字段和值会按风险颜色标记
- `--workers`：按表并发扫描数量，默认 `1`，即不启用并发；例如 `--workers 4`
- `--timeout`：单查询超时，默认 15s
- `--sql`：执行自定义 SQL；按需求原样执行，不限制为只读

## 支持数据库类型

- 原生支持：`mysql`、`mariadb`、`mssql`、`sqlserver`、`postgres`、`postgresql`、`oracle`、`go-ora`
- MySQL 协议兼容：`tidb`、`oceanbase`、`oceanbase-mysql`、`polardb-mysql`、`doris`、`starrocks`、`gbase-mysql`
- PostgreSQL 协议兼容：`opengauss`、`gaussdb`、`kingbase`、`kingbasees`、`highgo`、`polardb-postgres`
- 默认端口：MySQL 协议族 `3306`，MSSQL `1433`，PostgreSQL 协议族 `5432`，Oracle `1521`

协议兼容数据库会复用 MySQL 或 PostgreSQL 的连接协议、代理拨号和元数据扫描方式。达梦 DM、GBase 8s、神通等需要专用驱动、ODBC、CGO 或无法确认代理拨号能力的数据库暂不内置，避免破坏当前多平台发布包。

## 密码说明

- 数据库密码可以通过 `--password` 传入，也可以省略该参数后在提示中隐藏输入。
- 如果密码包含空格、`&`、`?`、`!` 等 shell 特殊字符，建议用单引号包裹，例如 `--password 'pa ss!word'`。
- SOCKS5 代理支持用户名密码：`--proxy socks5://user:pass@host:port`。
- HTTP CONNECT 代理支持用户名密码：`--proxy http://user:pass@host:port`。
- 代理账号或密码里如果包含 `@`、`:`、`/`、`?`、`#` 等 URL 特殊字符，需要 URL 编码，例如 `@` 写成 `%40`，`:` 写成 `%3A`。
- 为避免密码进入 shell 历史记录，生产或共享终端环境中建议省略数据库 `--password`，改用隐藏交互输入。

## 效果预览

以下截图中的数据库、表名和敏感样例值均为演示数据，不包含真实生产信息。

![终端扫描连接信息](assets/image-20260515125337865.png)

![敏感字段与样例值](assets/image-20260515125452703.png)

![并发扫描进度](assets/image-20260515125511339.png)

![Excel 输出结果](assets/image-20260515125542123.png)

## 注意

默认会完整展示敏感样例值，用于截图证明数据存在；如需降低暴露风险，请加 `--mask`。
扫描过程中按 `Ctrl+C` 会停止后续扫描，并尽量输出和写入已经扫描完成的表结果。
GitHub Actions 仅在推送 `v*` tag 时触发自动测试、构建和 Release；普通 main push 不触发发布流程。
