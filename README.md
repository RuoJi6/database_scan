# database_scan

`database_scan` 是一个 Go 编写的数据库敏感信息检索 CLI 工具，用于检查开发数据库中是否存在手机号、身份证、地址、账号、密码、邮箱、银行卡、token/secret 等敏感信息。默认终端表格输出，便于截图提交给开发部门。

## 支持能力

- 数据库：MySQL/MariaDB/TiDB、MSSQL、PostgreSQL
- 代理：直连、SOCKS5、HTTP CONNECT
- 认证：命令行密码或隐藏交互输入
- 输出：连接信息、命中汇总、最多 15 条完整样例数据
- 检索模式：
  - `field-content`：根据表/字段名定位敏感字段，再检索字段内容
  - `field-name`：只检索敏感表名/字段名
  - `content`：扫描字段内容
  - `all`：执行全部模式

## 构建

```bash
go build -o database_scan ./cmd/database_scan
```

## 使用示例

```bash
./database_scan --type mysql --host 127.0.0.1 --port 3306 --user root --password pass
```

```bash
./database_scan --type mssql --host 10.0.0.5 --user sa --password pass --proxy socks5://127.0.0.1:1080 --mode all
```

```bash
./database_scan --type postgres --host 10.0.0.8 --user dev --password pass --mode content --limit 15
```

```bash
./database_scan --type mysql --host 127.0.0.1 --user root --password pass --sql "select user, host from mysql.user"
```

## 参数

- `--type mysql|mssql|postgres`：数据库类型
- `--host` / `--port`：目标地址和端口，端口不填时使用默认端口
- `--user` / `--password`：账号密码；密码不填时交互输入
- `--database`：初始数据库；MySQL/MSSQL 下会优先扫描该库，PostgreSQL 会按库重连扫描
- `--proxy socks5://...|http://...`：代理地址
- `--mode field-content|field-name|content|all`：检索模式，默认 `field-content`
- `--limit`：最多展示样例数，默认 15
- `--include-system`：包含系统库
- `--mask`：样例值脱敏显示
- `--workers`：扫描并发，默认 4
- `--timeout`：单查询超时，默认 15s
- `--sql`：执行自定义 SQL；按需求原样执行，不限制为只读

## 注意

默认会完整展示敏感样例值，用于截图证明数据存在；如需降低暴露风险，请加 `--mask`。
