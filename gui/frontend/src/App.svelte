<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    ChooseFscanFile,
    ChooseOutputPath,
    GetDefaults,
    GetScanState,
    GetSupportedDatabaseTypes,
    OpenOutputFolder,
    ParseFscanFile,
    ParseFscanText,
    RunCustomSQL,
    StartScan,
    StopScan,
    TestConnection
  } from '../wailsjs/go/main/App.js';

  type ScanRequest = {
    Type: string;
    Host: string;
    Port: number;
    User: string;
    Password: string;
    Database: string;
    Table: string;
    Proxy: string;
    Mode: string;
    Level: string;
    Limit: number;
    SQL: string;
    Output: string;
    Fscan: string;
    FscanText: string;
    SplitOutput: boolean;
    IncludeSystem: boolean;
    Mask: boolean;
    Workers: number;
    Timeout: string;
  };

  type FieldResult = { Name: string; Kinds: string[]; Level: string; Mode: string; Total: number };
  type RowSample = { Values: Record<string, string> };
  type DisplaySampleRow = { Table: TableResult; Values: Record<string, string> };
  type SampleGroup = { Table: TableResult; Headers: string[]; Rows: DisplaySampleRow[] };
  type EvidenceField = FieldResult & { TableLabel: string; Database: string };
  type TableResult = {
    Database: string;
    Schema: string;
    Name: string;
    Total: number;
    Columns: string[];
    Fields: FieldResult[];
    Rows: RowSample[];
  };
  type LogEntry = { Time: string; Level: string; Message: string };
  type SQLResult = { Columns: string[]; Rows: string[][]; Total: number; Shown: number; Affected: number; IsQuery: boolean };
  type ConnectionTestResult = {
    Success: boolean;
    Message: string;
    Type: string;
    Host: string;
    Port: number;
    Database: string;
    User: string;
    Proxy: string;
    Version: string;
    ResolvedAddr: string;
    ServerTime: string;
  };
  type ScanState = {
    JobID: string;
    Status: string;
    Progress: number;
    Message: string;
    TargetLabel: string;
    Request: ScanRequest;
    Result: { Tables: TableResult[]; Errors: string[] };
    SQLResult?: SQLResult;
    Outputs: string[];
    Logs: LogEntry[];
    Errors: string[];
    StartedAt: string;
    FinishedAt: string;
  };
  type FscanPreview = { Total: number; Targets: Array<{ Type: string; Host: string; Port: number; User: string; Line: number; Raw: string; Password: boolean }> };
  type ManualTarget = {
    ID: number;
    Type: string;
    Host: string;
    Port: number;
    User: string;
    Password: string;
    ShowPassword: boolean;
  };

  const fallbackDefaults: ScanRequest = {
    Type: 'mysql',
    Host: '127.0.0.1',
    Port: 3306,
    User: '',
    Password: '',
    Database: '',
    Table: '',
    Proxy: '',
    Mode: 'field-content',
    Level: 'all',
    Limit: 15,
    SQL: '',
    Output: '',
    Fscan: '',
    FscanText: '',
    SplitOutput: false,
    IncludeSystem: false,
    Mask: false,
    Workers: 1,
    Timeout: '15s'
  };

  const emptyState: ScanState = {
    JobID: '',
    Status: 'idle',
    Progress: 0,
    Message: '等待扫描任务',
    TargetLabel: '',
    Request: fallbackDefaults,
    Result: { Tables: [], Errors: [] },
    Outputs: [],
    Logs: [],
    Errors: [],
    StartedAt: '',
    FinishedAt: ''
  };

  let activePage: 'single' | 'fscan' | 'sql' = 'single';
  let request: ScanRequest = { ...fallbackDefaults };
  let state: ScanState = emptyState;
  let dbTypes = [
    'mysql', 'mariadb', 'tidb', 'oceanbase', 'polardb-mysql', 'doris', 'starrocks', 'gbase-mysql',
    'mssql',
    'postgres', 'opengauss', 'gaussdb', 'kingbase', 'highgo', 'polardb-postgres',
    'oracle',
    'redis'
  ];
  let selectedTableIndex = -1;
  let pollTimer: number | undefined;
  let formError = '';
  let fscanPreview: FscanPreview = { Total: 0, Targets: [] };
  let manualTargetSeq = 1;
  let manualTargets: ManualTarget[] = [];
  let sqlResult: SQLResult | undefined;
  let showPassword = false;
  let sampleValueQuery = '';
  let sampleFieldQuery = '';
  let sampleSearchOp: 'and' | 'or' | 'not' = 'and';
  let resultPanelHeight = 260;
  let resizingResultPanel = false;
  let evidencePanelHeight = 540;
  let resizingEvidencePanel = false;
  let testingConnection = false;
  let connectionTest: ConnectionTestResult | undefined;

  const defaultPorts: Record<string, number> = {
    mysql: 3306,
    mariadb: 3306,
    tidb: 3306,
    oceanbase: 3306,
    'oceanbase-mysql': 3306,
    'polardb-mysql': 3306,
    doris: 3306,
    starrocks: 3306,
    'gbase-mysql': 3306,
    postgres: 5432,
    postgresql: 5432,
    opengauss: 5432,
    gaussdb: 5432,
    kingbase: 5432,
    kingbasees: 5432,
    highgo: 5432,
    'polardb-postgres': 5432,
    mssql: 1433,
    sqlserver: 1433,
    oracle: 1521,
    'go-ora': 1521,
    redis: 6379
  };

  manualTargets = [createManualTarget()];

  $: tables = state.Result?.Tables ?? [];
  $: selectedTable = selectedTableIndex >= 0 ? tables[selectedTableIndex] : undefined;
  $: sampleScopeTables = selectedTable ? [selectedTable] : tables;
  $: sampleGroups = sampleGroupsFor(sampleScopeTables);
  $: filteredSampleGroups = filterSampleGroups(sampleGroups, sampleValueQuery, sampleFieldQuery, sampleSearchOp);
  $: sampleRowsCount = sampleGroups.reduce((count, group) => count + group.Rows.length, 0);
  $: filteredSampleRowsCount = filteredSampleGroups.reduce((count, group) => count + group.Rows.length, 0);
  $: globalEvidenceFields = evidenceFieldsFor(tables);
  $: isRunning = state.Status === 'running';
  $: statusText = statusLabel(state.Status);
  $: risk = riskTotals(tables);
  $: currentTargetLabel = targetLabelForCurrentPage();

  onMount(async () => {
    if (hasWailsRuntime()) {
      request = normalizeGuiDefaults({ ...fallbackDefaults, ...(await GetDefaults()) });
      dbTypes = await GetSupportedDatabaseTypes();
    } else {
      state = browserDemoState('idle');
    }
  });

  onDestroy(() => {
    stopPolling();
  });

  function hasWailsRuntime() {
    return Boolean((window as any).go?.main?.App);
  }

  function beginResizeResultPanel(event: PointerEvent) {
    resizingResultPanel = true;
    event.preventDefault();
  }

  function beginResizeEvidencePanel(event: PointerEvent) {
    resizingEvidencePanel = true;
    event.preventDefault();
  }

  function resizeResultPanel(event: PointerEvent) {
    if (resizingResultPanel) {
      const stage = document.querySelector('.center-stage')?.getBoundingClientRect();
      if (!stage) return;
      const nextHeight = Math.round(event.clientY - stage.top - 86 - 86 - 20);
      resultPanelHeight = Math.max(150, Math.min(520, nextHeight));
      return;
    }
    if (resizingEvidencePanel) {
      const rail = document.querySelector('.right-rail')?.getBoundingClientRect();
      if (!rail) return;
      const nextHeight = Math.round(event.clientY - rail.top);
      evidencePanelHeight = Math.max(160, Math.min(Math.max(180, rail.height - 120), nextHeight));
    }
  }

  function stopResizeResultPanel() {
    resizingResultPanel = false;
    resizingEvidencePanel = false;
  }

  async function startScan() {
    formError = '';
    sqlResult = undefined;
    selectedTableIndex = -1;
    const next = requestForPage();
    const validationError = validateRequest(next, activePage);
    if (validationError) {
      formError = validationError;
      return;
    }
    if (!hasWailsRuntime()) {
      state = browserDemoState('running');
      window.setTimeout(() => (state = browserDemoState('completed')), 900);
      return;
    }
    try {
      state = await StartScan(next);
      startPolling();
    } catch (error) {
      formError = normalizeError(error);
    }
  }

  async function stopScan() {
    if (!hasWailsRuntime()) {
      state = browserDemoState('stopped');
      return;
    }
    state = await StopScan(state.JobID);
    stopPolling();
  }

  async function runSQL() {
    formError = '';
    sqlResult = undefined;
    const next = requestForPage();
    const validationError = validateRequest(next, 'sql');
    if (validationError) {
      formError = validationError;
      return;
    }
    if (!hasWailsRuntime()) {
      sqlResult = {
        Columns: ['id', 'email', 'phone'],
        Rows: [['1', 'audit@example.internal', '13800138000']],
        Total: 1,
        Shown: 1,
        Affected: 0,
        IsQuery: true
      };
      return;
    }
    try {
      sqlResult = await RunCustomSQL(next);
    } catch (error) {
      formError = normalizeError(error);
    }
  }

  async function testCurrentConnection() {
    formError = '';
    connectionTest = undefined;
    const next = connectionRequestFrom(request);
    const validationError = validateRequest(next, activePage === 'sql' ? 'sql' : 'single');
    if (validationError && !validationError.includes('SQL 原文')) {
      formError = validationError;
      return;
    }
    await runConnectionTest(next);
  }

  async function testManualTarget(index: number) {
    formError = '';
    connectionTest = undefined;
    const target = manualTargets[index];
    if (!target) return;
    const rowError = validateOneManualTarget(target, index + 1);
    if (rowError) {
      formError = rowError;
      return;
    }
    await runConnectionTest(connectionRequestFrom({
      ...request,
      Type: target.Type,
      Host: target.Host,
      Port: Number(target.Port),
      User: target.Type === 'redis' ? '' : target.User,
      Password: target.Password
    }));
  }

  async function runConnectionTest(next: ScanRequest) {
    testingConnection = true;
    try {
      if (!hasWailsRuntime()) {
        connectionTest = {
          Success: true,
          Message: next.Proxy ? '浏览器预览：代理连接测试通过' : '浏览器预览：连接测试通过',
          Type: next.Type,
          Host: next.Host,
          Port: next.Port,
          Database: next.Database || '-',
          User: next.User || '-',
          Proxy: next.Proxy || '',
          Version: 'preview',
          ResolvedAddr: next.Host,
          ServerTime: '-'
        };
        return;
      }
      connectionTest = await TestConnection(next);
    } catch (error) {
      formError = normalizeError(error);
    } finally {
      testingConnection = false;
    }
  }

  async function chooseFscanFile() {
    if (!hasWailsRuntime()) return;
    const path = await ChooseFscanFile();
    if (path) {
      request.Fscan = path;
      await parseFscan();
    }
  }

  async function chooseOutputPath() {
    if (!hasWailsRuntime()) {
      request.Output = '/tmp/database_scan_report.xlsx';
      return;
    }
    const path = await ChooseOutputPath();
    if (path) request.Output = path;
  }

  async function parseFscan() {
    formError = '';
    fscanPreview = { Total: 0, Targets: [] };
    if (hasManualTargetInput()) {
      await parseManualTargets();
      return;
    }
    if (!request.Fscan) return;
    if (!hasWailsRuntime()) {
      fscanPreview = {
        Total: 2,
        Targets: [
          { Type: 'mysql', Host: '10.211.55.16', Port: 3306, User: 'root', Line: 1, Raw: 'mysql 10.211.55.16:3306 root:pass', Password: true },
          { Type: 'redis', Host: '10.211.55.16', Port: 6379, User: '', Line: 2, Raw: 'redis 10.211.55.16:6379 pass', Password: true }
        ]
      };
      return;
    }
    try {
      fscanPreview = await ParseFscanFile(request.Fscan);
    } catch (error) {
      formError = normalizeError(error);
    }
  }

  async function parseManualTargets() {
    formError = '';
    fscanPreview = { Total: 0, Targets: [] };
    const validationError = validateManualTargets();
    if (validationError) {
      formError = validationError;
      return;
    }
    const text = manualTargetsText();
    request.FscanText = text;
    request.Fscan = '';
    if (!text) return;
    if (!hasWailsRuntime()) {
      fscanPreview = {
        Total: manualTargets.filter(manualTargetHasInput).length,
        Targets: manualTargets.filter(manualTargetHasInput).map((target, index) => ({
          Type: target.Type,
          Host: target.Host,
          Port: Number(target.Port),
          User: target.Type === 'redis' ? '' : target.User,
          Line: index + 1,
          Raw: manualTargetLine(target),
          Password: Boolean(target.Password)
        }))
      };
      return;
    }
    try {
      fscanPreview = await ParseFscanText(text);
    } catch (error) {
      formError = normalizeError(error);
    }
  }

  async function openFirstOutput() {
    const first = state.Outputs?.[0];
    if (first && hasWailsRuntime()) await OpenOutputFolder(first);
  }

  function startPolling() {
    stopPolling();
    pollTimer = window.setInterval(async () => {
      const next = await GetScanState(state.JobID);
      state = next;
      if (next.Status !== 'running') stopPolling();
    }, 700);
  }

  function stopPolling() {
    if (pollTimer) window.clearInterval(pollTimer);
    pollTimer = undefined;
  }

  function requestForPage(): ScanRequest {
    const next = { ...request };
    if (activePage === 'fscan') {
      next.Type = '';
      next.Host = '';
      next.User = '';
      next.Password = '';
      next.SQL = '';
      next.FscanText = manualTargetsText();
      if (next.FscanText) next.Fscan = '';
    } else {
      next.Fscan = '';
      next.FscanText = '';
      next.SplitOutput = false;
    }
    if (activePage !== 'sql') next.SQL = '';
    return next;
  }

  function connectionRequestFrom(source: ScanRequest): ScanRequest {
    return {
      ...source,
      Fscan: '',
      FscanText: '',
      SQL: '',
      Output: '',
      SplitOutput: false
    };
  }

  function validateRequest(next: ScanRequest, page: 'single' | 'fscan' | 'sql') {
    if (page === 'fscan') {
      const manualError = validateManualTargets();
      if (manualError) return manualError;
      if (!next.Fscan?.trim() && !next.FscanText?.trim()) return '请选择 fscan 结果文件或填写批量目标清单';
      if (next.SplitOutput && !next.Output?.trim()) return '按目标拆分 Excel 需要先填写输出文件路径';
      return '';
    }
    if (!next.Type?.trim()) return '请选择数据库类型';
    if (!next.Host?.trim()) return '请填写 Host';
    if (next.Type !== 'redis' && !next.User?.trim()) return '请填写账号；Redis 可留空';
    if (next.Table?.trim() && !next.Database?.trim()) return '指定表时必须同时填写指定库';
    if (!next.Limit || next.Limit <= 0) return '样例 limit 必须大于 0';
    if (!next.Workers || next.Workers <= 0) return '并发 workers 必须大于 0';
    if (page === 'sql' && !next.SQL?.trim()) return '请填写 SQL 原文';
    return '';
  }

  function normalizeError(error: unknown) {
    const message = String(error);
    if (message.includes('type, host and user are required')) return '请检查数据库类型、Host 和账号是否已填写；Redis 账号可以留空。';
    if (message.includes('sql is required')) return '请填写 SQL 原文。';
    if (message.includes('split-output requires output')) return '按目标拆分 Excel 需要先填写输出文件路径。';
    if (message.includes('table requires database')) return '指定表时必须同时填写指定库。';
    return message;
  }

  function normalizeGuiDefaults(next: ScanRequest) {
    if (!next.Host?.trim()) next.Host = fallbackDefaults.Host;
    if (!next.Port) next.Port = fallbackDefaults.Port;
    if (!next.Limit) next.Limit = 15;
    return next;
  }

  function changeType() {
    const nextPort = defaultPorts[request.Type] ?? 0;
    request.Port = nextPort;
    if (request.Type === 'redis') request.User = '';
  }

  function createManualTarget(type = 'mysql'): ManualTarget {
    const dbType = type || 'mysql';
    return {
      ID: manualTargetSeq++,
      Type: dbType,
      Host: '',
      Port: defaultPorts[dbType] ?? 0,
      User: dbType === 'redis' ? '' : '',
      Password: '',
      ShowPassword: false
    };
  }

  function insertManualTarget(afterIndex: number) {
    const baseType = manualTargets[afterIndex]?.Type || manualTargets[afterIndex - 1]?.Type || 'mysql';
    const next = createManualTarget(baseType);
    manualTargets = [...manualTargets.slice(0, afterIndex), next, ...manualTargets.slice(afterIndex)];
  }

  function removeManualTarget(index: number) {
    if (manualTargets.length <= 1) {
      manualTargets = [createManualTarget(manualTargets[0]?.Type || 'mysql')];
      fscanPreview = { Total: 0, Targets: [] };
      request.FscanText = '';
      return;
    }
    manualTargets = manualTargets.filter((_, itemIndex) => itemIndex !== index);
  }

  function changeManualTargetType(index: number) {
    const target = manualTargets[index];
    if (!target) return;
    target.Port = defaultPorts[target.Type] ?? target.Port;
    if (target.Type === 'redis') target.User = '';
    manualTargets = [...manualTargets];
  }

  function toggleManualTargetPassword(index: number) {
    const target = manualTargets[index];
    if (!target) return;
    target.ShowPassword = !target.ShowPassword;
    manualTargets = [...manualTargets];
  }

  function manualTargetHasInput(target: ManualTarget) {
    return Boolean(target.Host?.trim() || target.User?.trim() || target.Password?.trim());
  }

  function hasManualTargetInput() {
    return manualTargets.some((target) => target.Host?.trim() || target.User?.trim() || target.Password?.trim());
  }

  function validateManualTargets() {
    const activeTargets = manualTargets.filter(manualTargetHasInput);
    if (!activeTargets.length) return '';
    for (const [index, target] of activeTargets.entries()) {
      const error = validateOneManualTarget(target, index + 1);
      if (error) return error;
    }
    return '';
  }

  function validateOneManualTarget(target: ManualTarget, rowNo: number) {
    if (!target.Type?.trim()) return `第 ${rowNo} 个目标请选择数据库类型`;
    if (!target.Host?.trim()) return `第 ${rowNo} 个目标请填写 Host`;
    if (!target.Port || Number(target.Port) <= 0) return `第 ${rowNo} 个目标请填写有效端口`;
    if (target.Type !== 'redis' && !target.User?.trim()) return `第 ${rowNo} 个目标请填写账号`;
    return '';
  }

  function manualTargetsText() {
    const validationError = validateManualTargets();
    if (validationError) return '';
    return manualTargets.filter(manualTargetHasInput).map(manualTargetLine).filter(Boolean).join('\n');
  }

  function manualTargetLine(target: ManualTarget) {
    const host = target.Host.trim();
    const port = Number(target.Port);
    if (!host || !port) return '';
    if (target.Type === 'redis' && !target.User.trim()) {
      const password = target.Password.trim();
      return password ? `${target.Type} ${host}:${port} ${password}` : `${target.Type} ${host}:${port}`;
    }
    return `${target.Type} ${host}:${port} ${target.User.trim()}:${target.Password ?? ''}`;
  }

  function targetLabelForCurrentPage() {
    if (state.TargetLabel) return state.TargetLabel;
    if (activePage === 'fscan') {
      if (hasManualTargetInput()) return '手工多目标';
      return request.Fscan || '未设置';
    }
    return request.Host || '未设置';
  }

  function sampleGroupsFor(items: TableResult[]): SampleGroup[] {
    return items
      .map((table) => ({
        Table: table,
        Headers: sampleHeadersForTable(table),
        Rows: (table.Rows ?? []).map((row) => ({ Table: table, Values: row.Values ?? {} }))
      }))
      .filter((group) => group.Rows.length > 0);
  }

  function sampleHeadersForTable(table: TableResult) {
    const headers: string[] = [];
    const seen = new Set<string>();
    const add = (column: string) => {
      if (!seen.has(column)) {
        seen.add(column);
        headers.push(column);
      }
    };
    for (const column of table.Columns ?? []) add(column);
    for (const row of table.Rows ?? []) {
      for (const column of Object.keys(row.Values ?? {})) add(column);
    }
    return headers;
  }

  function filterSampleGroups(groups: SampleGroup[], valueQuery: string, fieldQuery: string, searchOp: 'and' | 'or' | 'not') {
    return groups
      .map((group) => ({
        ...group,
        Rows: filterSampleRows(group.Rows, group.Headers, valueQuery, fieldQuery, searchOp)
      }))
      .filter((group) => group.Rows.length > 0);
  }

  function fieldForSampleHeader(header: string, row?: DisplaySampleRow) {
    const table = row?.Table ?? selectedTable;
    return (table?.Fields ?? []).find((field) => sampleHeaderMatchesField(header, field));
  }

  function sampleCellClass(header: string, row?: DisplaySampleRow) {
    const field = fieldForSampleHeader(header, row);
    return field ? fieldLevel(field) : '';
  }

  function sampleHeaderClass(header: string, table?: TableResult) {
    const source = table ? (table.Fields ?? []) : sampleScopeTables.flatMap((item) => item.Fields ?? []);
    const fields = source.filter((field) => sampleHeaderMatchesField(header, field));
    if (fields.some((field) => fieldLevel(field) === 'high')) return 'high';
    if (fields.some((field) => fieldLevel(field) === 'medium')) return 'medium';
    if (fields.some((field) => fieldLevel(field) === 'low')) return 'low';
    return '';
  }

  function sampleHeaderMatchesField(header: string, field: FieldResult) {
    const normalizedHeader = header.trim().toLowerCase();
    const normalizedField = field.Name.trim().toLowerCase();
    if (normalizedHeader === normalizedField) return true;
    if (normalizedField === 'value' && ['value', '命中类型', '敏感级别', '判断依据'].includes(normalizedHeader)) return true;
    if (normalizedField === 'key' && ['key', 'path/field'].includes(normalizedHeader)) return true;
    return false;
  }

  function filterSampleRows(rows: DisplaySampleRow[], headers: string[], valueQuery: string, fieldQuery: string, searchOp: 'and' | 'or' | 'not') {
    const valueNeedle = valueQuery.trim().toLowerCase();
    const fieldNeedle = fieldQuery.trim().toLowerCase();
    if (!valueNeedle && !fieldNeedle) return rows;
    return rows.filter((row) => {
      const valueMatch = !valueNeedle || headers.some((header) => String(row.Values?.[header] ?? '').toLowerCase().includes(valueNeedle));
      const fieldMatch =
        !fieldNeedle ||
        headers.some((header) => header.toLowerCase().includes(fieldNeedle) && String(row.Values?.[header] ?? '').trim() !== '');
      if (valueNeedle && fieldNeedle) {
        if (searchOp === 'or') return valueMatch || fieldMatch;
        if (searchOp === 'not') return valueMatch && !fieldMatch;
        return valueMatch && fieldMatch;
      }
      if (valueNeedle) return searchOp === 'not' ? !valueMatch : valueMatch;
      return searchOp === 'not' ? !fieldMatch : fieldMatch;
    });
  }

  function tableLabel(table: TableResult) {
    return table.Schema ? `${table.Schema}.${table.Name}` : table.Name;
  }

  function evidenceFieldsFor(items: TableResult[]): EvidenceField[] {
    return items.flatMap((table) =>
      (table.Fields ?? []).map((field) => ({
        ...field,
        Database: table.Database,
        TableLabel: tableLabel(table)
      }))
    );
  }

  function statusLabel(status: string) {
    const labels: Record<string, string> = {
      idle: '待扫描',
      running: '扫描中',
      completed: '已完成',
      stopped: '已停止',
      failed: '失败'
    };
    return labels[status] ?? status;
  }

  function modeLabel(mode: string) {
    const labels: Record<string, string> = {
      'field-content': '字段名+内容',
      'field-name': '字段名',
      content: '内容正则',
      all: '全部'
    };
    return labels[mode] ?? mode;
  }

  function levelLabel(level: string) {
    const labels: Record<string, string> = { high: '高敏', medium: '中敏', low: '低敏', all: '全部' };
    return labels[level] ?? level;
  }

  function fieldLevel(field: FieldResult) {
    if (field.Level && field.Level !== 'all') return field.Level;
    const kinds = (field.Kinds ?? []).join('/');
    if (kinds.includes('密码') || kinds.includes('身份证') || kinds.includes('银行卡')) return 'high';
    if (kinds.includes('手机号') || kinds.includes('邮箱')) return 'medium';
    return 'low';
  }

  function riskTotals(items: TableResult[]) {
    return items.reduce(
      (acc, table) => {
        for (const field of table.Fields ?? []) acc[fieldLevel(field)] += 1;
        return acc;
      },
      { high: 0, medium: 0, low: 0 } as Record<string, number>
    );
  }

  function browserDemoState(status: string): ScanState {
    const progress = status === 'completed' ? 100 : status === 'running' ? 54 : status === 'stopped' ? 31 : 0;
    return {
      ...emptyState,
      Status: status,
      Progress: progress,
      Message: status === 'idle' ? '浏览器预览模式：Wails API 不可用' : statusLabel(status),
      Request: request,
      Outputs: status === 'completed' ? ['/tmp/database_scan_report.xlsx'] : [],
      Result: {
        Errors: status === 'stopped' ? ['用户停止扫描，保留已完成表结果'] : [],
        Tables: [
          {
            Database: 'audit_lab',
            Schema: '',
            Name: 'access_tokens',
            Total: 2,
            Columns: ['id', 'service_name', 'secret_key', 'refresh_token', 'owner_email'],
            Fields: [
              { Name: 'id', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'service_name', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'secret_key', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'refresh_token', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'owner_email', Kinds: ['密码/密钥', '邮箱'], Level: 'high', Mode: 'field-content', Total: 2 }
            ],
            Rows: [
              { Values: { id: '1', service_name: 'billing', secret_key: 'sk_live_mysql_demo_abcdef', refresh_token: 'rt_mysql_refresh_123456', owner_email: 'ops@example.internal' } },
              { Values: { id: '2', service_name: 'crm', secret_key: 'ak_mysql_crm_abcdef', refresh_token: 'rt_mysql_refresh_654321', owner_email: 'security@example.internal' } }
            ]
          },
          {
            Database: 'audit_lab',
            Schema: '',
            Name: 'customer_profile',
            Total: 2,
            Columns: ['customer_id', 'real_name', 'id_card_no', 'mobile_phone', 'email', 'bank_card', 'api_token', 'home_address'],
            Fields: [
              { Name: 'id_card_no', Kinds: ['身份证', '银行卡'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'mobile_phone', Kinds: ['手机号'], Level: 'medium', Mode: 'field-content', Total: 2 },
              { Name: 'email', Kinds: ['邮箱'], Level: 'medium', Mode: 'field-content', Total: 2 },
              { Name: 'bank_card', Kinds: ['银行卡'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'api_token', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'home_address', Kinds: ['地址'], Level: 'low', Mode: 'field-content', Total: 2 }
            ],
            Rows: [
              { Values: { customer_id: '1', real_name: 'Zhang San', id_card_no: '110101199003071234', mobile_phone: '13800138000', email: 'audit@example.internal', bank_card: '6222020202021234', api_token: 'tok_live_mysql_123456', home_address: 'Beijing Road 88' } },
              { Values: { customer_id: '2', real_name: 'Li Si', id_card_no: '310101198812121234', mobile_phone: '13900139000', email: 'risk@example.internal', bank_card: '6217001234567890', api_token: 'secret_mysql_abcdef', home_address: 'Shanghai Avenue 100' } }
            ]
          },
          {
            Database: 'audit_lab_archive',
            Schema: '',
            Name: 'customer_profile',
            Total: 2,
            Columns: ['id', 'real_name', 'id_card_no', 'mobile_phone', 'email', 'home_address'],
            Fields: [
              { Name: 'id_card_no', Kinds: ['身份证', '银行卡'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'mobile_phone', Kinds: ['手机号'], Level: 'medium', Mode: 'field-content', Total: 2 },
              { Name: 'email', Kinds: ['邮箱'], Level: 'medium', Mode: 'field-content', Total: 2 },
              { Name: 'home_address', Kinds: ['地址'], Level: 'low', Mode: 'field-content', Total: 2 }
            ],
            Rows: [
              { Values: { id: '1', real_name: 'Archive Carol', id_card_no: '110101198803033333', mobile_phone: '13700137003', email: 'archive.carol@example.internal', home_address: 'Guangzhou archive road 3' } },
              { Values: { id: '2', real_name: 'Archive Dave', id_card_no: '110101198904044444', mobile_phone: '13600136004', email: 'archive.dave@example.internal', home_address: 'Shenzhen archive road 4' } }
            ]
          },
          {
            Database: 'audit_lab_extra',
            Schema: '',
            Name: 'access_tokens',
            Total: 2,
            Columns: ['id', 'service_name', 'secret_key', 'refresh_token', 'owner_email'],
            Fields: [
              { Name: 'id', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'service_name', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'secret_key', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'refresh_token', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'owner_email', Kinds: ['密码/密钥', '邮箱'], Level: 'high', Mode: 'field-content', Total: 2 }
            ],
            Rows: [
              { Values: { id: '1', service_name: 'billing', secret_key: 'sk_live_extra_mysql_abcdef', refresh_token: 'rt_extra_mysql_123456', owner_email: 'billing-extra@example.internal' } },
              { Values: { id: '2', service_name: 'ops', secret_key: 'ak_extra_mysql_ops_secret', refresh_token: 'rt_extra_mysql_654321', owner_email: 'ops-extra@example.internal' } }
            ]
          },
          {
            Database: 'audit_lab_extra',
            Schema: '',
            Name: 'customer_profile',
            Total: 2,
            Columns: ['id', 'real_name', 'id_card_no', 'mobile_phone', 'email', 'home_address'],
            Fields: [
              { Name: 'id_card_no', Kinds: ['身份证', '银行卡'], Level: 'high', Mode: 'field-content', Total: 2 },
              { Name: 'mobile_phone', Kinds: ['手机号'], Level: 'medium', Mode: 'field-content', Total: 2 },
              { Name: 'email', Kinds: ['邮箱'], Level: 'medium', Mode: 'field-content', Total: 2 },
              { Name: 'home_address', Kinds: ['地址'], Level: 'low', Mode: 'field-content', Total: 2 }
            ],
            Rows: [
              { Values: { id: '1', real_name: 'Extra Alice', id_card_no: '110101199001011111', mobile_phone: '13800138001', email: 'extra.alice@example.internal', home_address: 'Beijing test road 1' } },
              { Values: { id: '2', real_name: 'Extra Bob', id_card_no: '110101199002022222', mobile_phone: '13900139002', email: 'extra.bob@example.internal', home_address: 'Shanghai test road 2' } }
            ]
          }
        ]
      },
      Logs: [
        { Time: new Date().toLocaleTimeString('zh-CN', { hour12: false }), Level: 'info', Message: '枚举字段并启动表级扫描' },
        { Time: new Date().toLocaleTimeString('zh-CN', { hour12: false }), Level: 'debug', Message: 'audit_lab / audit_lab_archive / audit_lab_extra 命中多库敏感字段' }
      ]
    };
  }

  function tableRisk(table: TableResult) {
    const levels = (table.Fields ?? []).map(fieldLevel);
    if (levels.includes('high')) return 'high';
    if (levels.includes('medium')) return 'medium';
    return levels.length ? 'low' : 'none';
  }

  function riskText(level: string) {
    return level === 'high' ? '高敏' : level === 'medium' ? '中敏' : level === 'low' ? '低敏' : '-';
  }
</script>

<svelte:window on:pointermove={resizeResultPanel} on:pointerup={stopResizeResultPanel} />

<main class="workbench">
  <header class="topbar">
    <div class="identity">
      <div class="product-mark">DBS</div>
      <div>
        <h1>database_scan 审计工作台</h1>
        <p>连接、扫描、证据、导出在同一条工作流里完成</p>
      </div>
    </div>
    <div class="session-strip">
      <span class="session-item">状态 <strong>{statusText}</strong></span>
      <span class="session-item">进度 <strong>{state.Progress || 0}%</strong></span>
      <span class="session-item">命中表 <strong>{tables.length}</strong></span>
      <span class="session-item danger">高敏 <strong>{risk.high}</strong></span>
      <span class="session-item warn">中敏 <strong>{risk.medium}</strong></span>
      <span class="session-item low">低敏 <strong>{risk.low}</strong></span>
    </div>
  </header>

  <section class="workspace">
    <aside class="left-rail">
      <section class="panel">
        <div class="panel-heading">
          <h2>工作模式</h2>
          <span>{activePage}</span>
        </div>
        <div class="tabs">
          <button class:active={activePage === 'single'} on:click={() => (activePage = 'single')}>单目标扫描</button>
          <button class:active={activePage === 'fscan'} on:click={() => (activePage = 'fscan')}>fscan 批量</button>
          <button class:active={activePage === 'sql'} on:click={() => (activePage = 'sql')}>自定义 SQL</button>
        </div>
      </section>

      {#if formError}
        <div class="error-line top-error">{formError}</div>
      {/if}

      {#if activePage !== 'fscan'}
        <section class="panel">
          <div class="panel-heading">
            <h2>连接信息</h2>
            <span>{request.Type || '-'}</span>
          </div>
          <div class="form-grid">
            <label>
              <span>数据库类型</span>
              <select bind:value={request.Type} on:change={changeType}>
                {#each dbTypes as type}
                  <option value={type}>{type}</option>
                {/each}
              </select>
            </label>
            <label>
              <span>端口</span>
              <input type="number" bind:value={request.Port} />
            </label>
            <label class="full">
              <span>Host</span>
              <input bind:value={request.Host} placeholder="127.0.0.1" />
            </label>
            <label class="full">
              <span>账号</span>
              <input bind:value={request.User} autocomplete="off" />
            </label>
            <label class="full">
              <span>密码</span>
              <div class="password-field">
                {#if showPassword}
                  <input bind:value={request.Password} type="text" autocomplete="off" />
                {:else}
                  <input bind:value={request.Password} type="password" autocomplete="off" />
                {/if}
                <button type="button" on:click={() => (showPassword = !showPassword)}>{showPassword ? '隐藏' : '查看'}</button>
              </div>
            </label>
            <label>
              <span>指定库</span>
              <input bind:value={request.Database} placeholder="audit_lab,other_db" />
            </label>
            <label>
              <span>指定表</span>
              <input bind:value={request.Table} placeholder="table_a,schema.table_b" />
            </label>
            <label class="full">
              <span>代理</span>
              <input bind:value={request.Proxy} placeholder="socks5://127.0.0.1:1080" />
            </label>
            <button class="full" type="button" on:click={testCurrentConnection} disabled={testingConnection}>
              {testingConnection ? '测试中' : request.Proxy ? '测试连接/代理' : '测试连接'}
            </button>
          </div>
          {#if connectionTest}
            <div class="connection-test-result">
              <strong>{connectionTest.Message}</strong>
              <span>{connectionTest.Type} · {connectionTest.Host}:{connectionTest.Port} · {connectionTest.Proxy || '直连'}</span>
            </div>
          {/if}
        </section>
      {:else}
        <section class="panel">
          <div class="panel-heading">
            <h2>fscan 结果</h2>
            <span>{fscanPreview.Total} targets</span>
          </div>
          <div class="control-list">
            <label>
              <span>结果文件</span>
              <input bind:value={request.Fscan} on:change={parseFscan} placeholder="/tmp/fscan_result.txt" />
            </label>
            <div class="button-row">
              <button on:click={chooseFscanFile}>选择文件</button>
              <button on:click={parseFscan}>解析预览</button>
            </div>
            <div class="manual-target-box">
              <div class="manual-target-heading">
                <span>手工多目标</span>
                <button type="button" on:click={() => insertManualTarget(manualTargets.length)}>插入目标</button>
              </div>
              <div class="manual-target-list">
                {#each manualTargets as target, index (target.ID)}
                  <div class="manual-target-row">
                    <div class="manual-target-grid">
                      <label>
                        <span>数据库</span>
                        <select bind:value={target.Type} on:change={() => changeManualTargetType(index)}>
                          {#each dbTypes as type}
                            <option value={type}>{type}</option>
                          {/each}
                        </select>
                      </label>
                      <label>
                        <span>端口</span>
                        <input type="number" min="1" bind:value={target.Port} />
                      </label>
                      <label class="wide">
                        <span>Host</span>
                        <input bind:value={target.Host} placeholder="127.0.0.1" />
                      </label>
                      <div class="manual-credential-grid">
                        <label>
                          <span>账号</span>
                          <input bind:value={target.User} disabled={target.Type === 'redis'} placeholder={target.Type === 'redis' ? 'Redis 可留空' : 'root'} />
                        </label>
                        <label>
                          <span>密码</span>
                          <div class="manual-password-field">
                            {#if target.ShowPassword}
                              <input bind:value={target.Password} type="text" autocomplete="off" />
                            {:else}
                              <input bind:value={target.Password} type="password" autocomplete="off" />
                            {/if}
                            <button type="button" on:click={() => toggleManualTargetPassword(index)}>{target.ShowPassword ? '隐藏' : '查看'}</button>
                          </div>
                        </label>
                      </div>
                    </div>
                    <div class="manual-target-actions">
                      <button type="button" on:click={() => testManualTarget(index)} disabled={testingConnection}>测试</button>
                      <button type="button" on:click={() => insertManualTarget(index + 1)}>插入</button>
                      <button type="button" on:click={() => removeManualTarget(index)}>删除</button>
                    </div>
                  </div>
                {/each}
              </div>
            </div>
            <div class="button-row">
              <button on:click={parseManualTargets}>解析手工目标</button>
              <button on:click={() => { manualTargets = [createManualTarget()]; request.FscanText = ''; fscanPreview = { Total: 0, Targets: [] }; }}>清空手工目标</button>
            </div>
            <label>
              <span>批量代理</span>
              <input bind:value={request.Proxy} placeholder="socks5://127.0.0.1:1080" />
            </label>
            {#if connectionTest}
              <div class="connection-test-result">
                <strong>{connectionTest.Message}</strong>
                <span>{connectionTest.Type} · {connectionTest.Host}:{connectionTest.Port} · {connectionTest.Proxy || '直连'}</span>
              </div>
            {/if}
            <label class="toggle-line">
              <input type="checkbox" bind:checked={request.SplitOutput} />
              <span>按目标拆分 Excel</span>
            </label>
          </div>
        </section>
      {/if}

      <section class="panel">
        <div class="panel-heading">
          <h2>扫描参数</h2>
          <span>{modeLabel(request.Mode)}</span>
        </div>
        <div class="control-list">
          <div class="split-inputs">
            <label>
              <span>模式</span>
              <select bind:value={request.Mode}>
                <option value="field-content">字段名+内容</option>
                <option value="field-name">字段名</option>
                <option value="content">内容正则</option>
                <option value="all">全部</option>
              </select>
            </label>
            <label>
              <span>敏感级别</span>
              <select bind:value={request.Level}>
                <option value="all">全部</option>
                <option value="high">高敏</option>
                <option value="medium">中敏</option>
                <option value="low">低敏</option>
              </select>
            </label>
          </div>
          <div class="split-inputs">
            <label>
              <span>整行样例条数</span>
              <input type="number" min="1" bind:value={request.Limit} />
            </label>
            <label>
              <span>并发 workers</span>
              <input type="number" min="1" bind:value={request.Workers} />
            </label>
          </div>
          <label>
            <span>单查询超时</span>
            <input bind:value={request.Timeout} placeholder="15s" />
          </label>
          <label>
            <span>Excel 输出</span>
            <input bind:value={request.Output} placeholder="/tmp/database_scan_report.xlsx" />
          </label>
          <div class="button-row">
            <button on:click={chooseOutputPath}>选择输出</button>
            <button on:click={openFirstOutput} disabled={!state.Outputs?.length}>打开目录</button>
          </div>
          <label class="toggle-line">
            <input type="checkbox" bind:checked={request.IncludeSystem} />
            <span>包含系统库</span>
          </label>
          <label class="toggle-line">
            <input type="checkbox" bind:checked={request.Mask} />
            <span>脱敏展示和导出样例</span>
          </label>
          {#if activePage === 'sql'}
            <label>
              <span>SQL 原文</span>
              <textarea bind:value={request.SQL} spellcheck="false" placeholder="SELECT * FROM users LIMIT 5"></textarea>
            </label>
            <div class="warning">自定义 SQL 将按原文执行。SELECT 会展示结果，非查询语句会返回影响行数。</div>
          {/if}
          <div class="actions">
            {#if activePage === 'sql'}
              <button class="primary" on:click={runSQL} disabled={isRunning}>确认执行 SQL</button>
            {:else}
              <button class="primary" on:click={startScan} disabled={isRunning}>开始扫描</button>
            {/if}
            <button on:click={stopScan} disabled={!isRunning}>停止</button>
          </div>
        </div>
      </section>
    </aside>

    <section class="center-stage" style={`grid-template-rows: 86px 86px ${resultPanelHeight}px 8px minmax(260px, 1fr);`}>
      <section class="scan-meter">
        <div>
          <span>当前目标</span>
          <strong>{currentTargetLabel}</strong>
        </div>
        <div class="meter-track"><span style={`width:${state.Progress || 0}%`}></span></div>
        <div class="meter-meta">
          <span>{state.Message}</span>
          <span>{request.Mode} / {levelLabel(request.Level)} / limit {request.Limit}</span>
        </div>
      </section>

      {#if activePage === 'fscan'}
        <section class="database-strip">
          {#each fscanPreview.Targets.slice(0, 8) as target}
            <button class="target-chip">
              <strong>{target.Type}</strong>
              <span>{target.Host}:{target.Port}</span>
              <em>{target.User || 'redis'}</em>
            </button>
          {:else}
            <span class="muted">选择 fscan 文件或填写手工目标清单后显示去重预览。</span>
          {/each}
        </section>
      {:else}
        <section class="database-strip">
          <div><span>数据库</span><strong>{request.Database || '全部可访问库'}</strong></div>
          <div><span>表过滤</span><strong>{request.Table || '未限制'}</strong></div>
          <div><span>代理</span><strong>{request.Proxy || '直连'}</strong></div>
          <div><span>导出</span><strong>{request.Output || '未设置'}</strong></div>
        </section>
      {/if}

      <section class="result-panel">
        <div class="panel-heading">
          <h2>命中结果</h2>
          <button class="mini-action" on:click={() => (selectedTableIndex = -1)} disabled={selectedTableIndex < 0}>显示全部样例</button>
        </div>
        <div class="result-table-wrap">
          <table class="result-table">
            <thead>
              <tr>
                <th>数据库</th>
                <th>表</th>
                <th>敏感字段</th>
                <th>存在行数</th>
                <th>风险</th>
              </tr>
            </thead>
            <tbody>
              {#each tables as table, index}
                <tr class:active={index === selectedTableIndex} on:click={() => (selectedTableIndex = index)}>
                  <td>{table.Database}</td>
                  <td>{tableLabel(table)}</td>
                  <td>{(table.Fields ?? []).map((f) => f.Name).join(', ')}</td>
                  <td>{table.Total}</td>
                  <td><span class={`risk-dot ${tableRisk(table)}`}>{riskText(tableRisk(table))}</span></td>
                </tr>
              {:else}
                <tr><td colspan="5" class="empty">暂无扫描结果。</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      </section>

      <button
        class="row-resizer"
        class:active={resizingResultPanel}
        type="button"
        title="拖动调整命中结果和整行样例高度"
        on:pointerdown={beginResizeResultPanel}
      ></button>

      <section class="panel sample-wide">
        <div class="panel-heading">
          <h2>整行样例</h2>
          <span>{sampleScopeTables.length ? `${selectedTable ? tableLabel(selectedTable) : '全部命中表'} · ${filteredSampleRowsCount}/${sampleRowsCount} rows` : '-'}</span>
        </div>
        {#if sampleScopeTables.length}
          <div class="sample-filter">
            <label>
              <span>样例值检索</span>
              <input bind:value={sampleValueQuery} placeholder="默认显示所有数据" />
            </label>
            <label>
              <span>字段检索</span>
              <input bind:value={sampleFieldQuery} placeholder="字段名，如 phone / id_card" />
            </label>
            <label>
              <span>关系</span>
              <select bind:value={sampleSearchOp}>
                <option value="and">与</option>
                <option value="or">或</option>
                <option value="not">非</option>
              </select>
            </label>
          </div>
          <div class="sample-scroll">
            {#each filteredSampleGroups as group}
              <section class="sample-group">
                <div class="sample-group-heading" title={`${group.Table.Database} / ${tableLabel(group.Table)}`}>
                  <strong>{group.Table.Database}</strong>
                  <span>{tableLabel(group.Table)}</span>
                  <em>{group.Rows.length}/{group.Table.Rows?.length ?? 0} rows</em>
                </div>
                <table>
                  <thead><tr>{#each group.Headers as header}<th class={sampleHeaderClass(header, group.Table)}>{header}</th>{/each}</tr></thead>
                  <tbody>
                    {#each group.Rows as row}
                      <tr>{#each group.Headers as header}<td class={`sample-cell ${sampleCellClass(header, row)}`}>{row.Values?.[header] ?? ''}</td>{/each}</tr>
                    {/each}
                  </tbody>
                </table>
              </section>
            {:else}
              <div class="empty-detail">没有匹配的样例数据。</div>
            {/each}
          </div>
        {:else}
          <div class="empty-detail">扫描完成后会汇总展示所有命中表的整行样例。</div>
        {/if}
      </section>
    </section>

    <aside class="right-rail" style={`grid-template-rows: ${evidencePanelHeight}px 8px minmax(120px, 1fr);`}>
      <section class="panel detail-panel">
        <div class="panel-heading">
          <h2>字段证据</h2>
          <span>{selectedTable ? `${selectedTable.Schema}.${selectedTable.Name}` : globalEvidenceFields.length ? '全部命中表' : '-'}</span>
        </div>
        {#if selectedTable}
          <div class="field-list">
            {#each selectedTable.Fields ?? [] as field}
              <div class={`field-row ${fieldLevel(field)}`}>
                <strong>{field.Name}</strong>
                <span>{(field.Kinds ?? []).join('/')} · {levelLabel(fieldLevel(field))} · {modeLabel(field.Mode)}</span>
                <em>存在行数 {field.Total}</em>
              </div>
            {/each}
          </div>
        {:else if globalEvidenceFields.length}
          <div class="field-list">
            {#each globalEvidenceFields as field}
              <div class={`field-row ${fieldLevel(field)}`}>
                <strong>{field.Name}</strong>
                <span>{field.Database} / {field.TableLabel}</span>
                <span>{(field.Kinds ?? []).join('/')} · {levelLabel(fieldLevel(field))} · {modeLabel(field.Mode)}</span>
                <em>存在行数 {field.Total}</em>
              </div>
            {/each}
          </div>
        {:else}
          <div class="empty-detail">选择左侧结果行查看字段证据。</div>
        {/if}
      </section>

      <button
        class="row-resizer"
        class:active={resizingEvidencePanel}
        type="button"
        title="拖动调整字段证据和 SQL 结果高度"
        on:pointerdown={beginResizeEvidencePanel}
      ></button>

      <section class="panel">
        <div class="panel-heading">
          <h2>SQL 结果</h2>
          <span>{sqlResult ? (sqlResult.IsQuery ? `${sqlResult.Shown}/${sqlResult.Total}` : `${sqlResult.Affected}`) : '-'}</span>
        </div>
        {#if sqlResult}
          {#if sqlResult.IsQuery}
            <div class="sql-table-wrap">
              <table>
                <thead><tr>{#each sqlResult.Columns as col}<th>{col}</th>{/each}</tr></thead>
                <tbody>{#each sqlResult.Rows as row}<tr>{#each row as cell}<td>{cell}</td>{/each}</tr>{/each}</tbody>
              </table>
            </div>
          {:else}
            <div class="affected">影响行数：{sqlResult.Affected}</div>
          {/if}
        {:else}
          <div class="empty-detail">自定义 SQL 的 SELECT 结果或 Exec 影响行数会显示在这里。</div>
        {/if}
      </section>
    </aside>
  </section>

  <footer class="bottom-console">
    <section>
      <div class="console-heading">
        <h2>实时日志</h2>
        <span>{state.Logs?.length ?? 0} lines</span>
      </div>
      <div class="log-stream">
        {#each state.Logs ?? [] as log}
          <div class={log.Level}><span>{log.Time}</span><strong>{log.Level}</strong>{log.Message}</div>
        {:else}
          <div><span>--:--:--</span><strong>idle</strong>等待任务。</div>
        {/each}
      </div>
    </section>
    <section>
      <div class="console-heading">
        <h2>扫描错误</h2>
        <span>{(state.Errors?.length ?? 0) + (state.Result?.Errors?.length ?? 0)}</span>
      </div>
      <div class="error-stream">
        {#each [...(state.Errors ?? []), ...(state.Result?.Errors ?? [])] as error}
          <div>{error}</div>
        {:else}
          <div class="muted">暂无错误。</div>
        {/each}
      </div>
    </section>
    <section>
      <div class="console-heading">
        <h2>导出状态</h2>
        <span>{state.Outputs?.length ?? 0} files</span>
      </div>
      <div class="output-list">
        {#each state.Outputs ?? [] as path}
          <button on:click={() => hasWailsRuntime() && OpenOutputFolder(path)}>{path}</button>
        {:else}
          <div class="muted">设置输出路径后，扫描完成会写入 xlsx。</div>
        {/each}
      </div>
    </section>
  </footer>
</main>
