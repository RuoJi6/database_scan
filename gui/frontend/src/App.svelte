<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    ChooseBackupExportPath,
    ChooseBackupImportFile,
    ChooseFscanFile,
    ChooseOutputPath,
    CreateTask,
    DeleteTask,
    ExportDataBackup,
    GetDefaults,
    GetSupportedDatabaseTypes,
    GetTask,
    GetVaultStatus,
    ImportDataBackup,
    ListTasks,
    OpenOutputFolder,
    ParseFscanFile,
    ParseFscanText,
    ResetVault,
    SetupVault,
    StartTask,
    StopTask,
    TestConnection,
    UnlockVault,
    UpdateTask
  } from '../wailsjs/go/main/App.js';

  type TaskKind = 'single' | 'fscan' | 'sql';
  type ViewMode = 'overview' | 'wizard' | 'detail';
  type DetailTab = 'hits' | 'fields' | 'targets' | 'sql' | 'samples' | 'logs';
  type ThemePreference = 'system' | 'light' | 'dark';

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
    TextEncoding: string;
    Workers: number;
    Timeout: string;
  };

  type FieldResult = { Name: string; Kinds: string[]; Level: string; Mode: string; Total: number };
  type EvidenceField = FieldResult & { Database: string; TableName: string; Table: TableResult };
  type RowSample = { Values: Record<string, string> };
  type SampleGroup = { Table: TableResult; Headers: string[]; Rows: Array<{ Values: Record<string, string> }> };
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
  type GUITask = {
    ID: string;
    Name: string;
    Description: string;
    Kind: TaskKind;
    Status: string;
    Progress: number;
    Message: string;
    TargetLabel: string;
    Request: ScanRequest;
    State: ScanState;
    CreatedAt: string;
    UpdatedAt: string;
    StartedAt: string;
    FinishedAt: string;
  };
  type VaultStatus = { Initialized: boolean; Unlocked: boolean; Path: string };
  type BackupResult = { Path: string; Encrypted: boolean; ExportedTasks: number; ImportedTasks: number; RenamedTasks: number; Message: string };
  type FscanPreview = { Total: number; Targets: Array<{ Type: string; Host: string; Port: number; User: string; Line: number; Raw: string; Password: boolean }> };
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
    TextEncoding: 'auto',
    Workers: 1,
    Timeout: '15s'
  };

  const textEncodingOptions = [
    ['auto', '自动修复'],
    ['utf8', 'UTF-8'],
    ['gbk', 'GBK / CP936'],
    ['gb18030', 'GB18030'],
    ['big5', 'Big5'],
    ['shift-jis', 'Shift-JIS'],
    ['euc-kr', 'EUC-KR'],
    ['latin1', 'Latin1 / ISO-8859-1'],
    ['windows-1252', 'Windows-1252']
  ];

  const emptyState: ScanState = {
    JobID: '',
    Status: 'draft',
    Progress: 0,
    Message: '等待配置任务',
    TargetLabel: '',
    Request: fallbackDefaults,
    Result: { Tables: [], Errors: [] },
    Outputs: [],
    Logs: [],
    Errors: [],
    StartedAt: '',
    FinishedAt: ''
  };

  const defaultPorts: Record<string, number> = {
    mysql: 3306,
    mariadb: 3306,
    tidb: 3306,
    oceanbase: 3306,
    'polardb-mysql': 3306,
    doris: 3306,
    starrocks: 3306,
    'gbase-mysql': 3306,
    mssql: 1433,
    postgres: 5432,
    opengauss: 5432,
    gaussdb: 5432,
    kingbase: 5432,
    highgo: 5432,
    'polardb-postgres': 5432,
    oracle: 1521,
    redis: 6379
  };

  let vaultStatus: VaultStatus = { Initialized: false, Unlocked: false, Path: '' };
  let password = '';
  let confirmPassword = '';
  let authError = '';
  let loading = true;
  let viewMode: ViewMode = 'overview';
  let tasks: GUITask[] = [];
  let selectedTask: GUITask | undefined;
  let taskSearch = '';
  let defaults: ScanRequest = { ...fallbackDefaults };
  let dbTypes = Object.keys(defaultPorts);
  let wizardStep = 1;
  let wizardEditingID = '';
  let draftName = '';
  let draftDescription = '';
  let draftKind: TaskKind = 'single';
  let draftRequest: ScanRequest = { ...fallbackDefaults };
  let wizardError = '';
  let formError = '';
  let activeTab: DetailTab = 'hits';
  let selectedEvidence: EvidenceField | undefined;
  let dataQuery = '';
  let sampleMetaQuery = '';
  let fieldQuery = '';
  let riskFilter = 'all';
  let showPassword = false;
  let manualSeq = 1;
  let manualTargets: ManualTarget[] = [createManualTarget()];
  let fscanPreview: FscanPreview = { Total: 0, Targets: [] };
  let connectionTest: ConnectionTestResult | undefined;
  let testingConnection = false;
  let manualTestingIndex = -1;
  let pollTimer: number | undefined;
  let showDataManager = false;
  let backupExportPath = '';
  let backupEncrypt = true;
  let backupPassword = '';
  let backupImportPath = '';
  let backupImportPassword = '';
  let backupBusy = false;
  let backupError = '';
  let backupResult: BackupResult | undefined;
  let activeTargetPopoverID = '';
  let themePreference: ThemePreference = 'system';
  let systemPrefersDark = false;
  let themeQuery: MediaQueryList | undefined;
  let copiedTaskID = '';
  let copyTimer: number | undefined;

  $: currentState = selectedTask?.State ?? emptyState;
  $: currentTables = currentState.Result?.Tables ?? [];
  $: currentRisk = riskTotals(currentTables);
  $: currentEvidence = evidenceFieldsFor(currentTables);
  $: outputPath = currentState.Outputs?.[0] || selectedTask?.Request.Output || '';
  $: showSQLTab = shouldShowSQLTab(selectedTask);
  $: if (activeTab === 'sql' && !showSQLTab) activeTab = 'hits';
  $: filteredTables = filterTables(currentTables, fieldQuery, riskFilter);
  $: sampleGroups = sampleGroupsFor(currentTables, dataQuery, sampleMetaQuery);
  $: sampleRowTotal = sampleGroups.reduce((total, group) => total + group.Rows.length, 0);
  $: activeTheme = themePreference === 'system' ? (systemPrefersDark ? 'dark' : 'light') : themePreference;
  $: applyTheme(activeTheme);
  $: filteredTasks = tasks.filter((task) => {
    const query = taskSearch.trim().toLowerCase();
    if (!query) return true;
    return [task.Name, task.Description, kindLabel(task.Kind), task.TargetLabel, task.Message].join(' ').toLowerCase().includes(query);
  });
  $: boardStats = taskStats(tasks);

  onMount(async () => {
    initTheme();
    if (!hasWailsRuntime()) {
      defaults = { ...fallbackDefaults };
      vaultStatus = { Initialized: true, Unlocked: true, Path: 'browser-preview' };
      tasks = [demoTask('completed'), demoTask('draft')];
      selectedTask = tasks[0];
      loading = false;
      return;
    }
    try {
      defaults = normalizeDefaults({ ...fallbackDefaults, ...(await GetDefaults()) });
      draftRequest = { ...defaults };
      dbTypes = await GetSupportedDatabaseTypes();
      vaultStatus = await GetVaultStatus();
      if (vaultStatus.Unlocked) await loadTasks();
    } catch (error) {
      authError = normalizeError(error);
    } finally {
      loading = false;
    }
  });

  onDestroy(() => {
    stopPolling();
    if (copyTimer) window.clearTimeout(copyTimer);
    if (themeQuery) themeQuery.removeEventListener('change', handleSystemThemeChange);
  });

  function initTheme() {
    const saved = window.localStorage.getItem('database-scan-theme') as ThemePreference | null;
    if (saved === 'light' || saved === 'dark' || saved === 'system') {
      themePreference = saved;
    }
    themeQuery = window.matchMedia('(prefers-color-scheme: dark)');
    systemPrefersDark = themeQuery.matches;
    themeQuery.addEventListener('change', handleSystemThemeChange);
    applyTheme(themePreference === 'system' ? (systemPrefersDark ? 'dark' : 'light') : themePreference);
  }

  function handleSystemThemeChange(event: MediaQueryListEvent) {
    systemPrefersDark = event.matches;
  }

  function handleThemeChange() {
    window.localStorage.setItem('database-scan-theme', themePreference);
    activeTargetPopoverID = '';
  }

  function applyTheme(theme: 'light' | 'dark') {
    if (typeof document === 'undefined') return;
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
  }

  function hasWailsRuntime() {
    return Boolean((window as any).go?.main?.App);
  }

  async function callTestConnection(request: ScanRequest) {
    if (hasWailsRuntime()) return TestConnection(request);
    return demoConnection(request);
  }

  async function setupVault() {
    authError = '';
    if (password.length < 6) {
      authError = '启动密码至少 6 位';
      return;
    }
    if (password !== confirmPassword) {
      authError = '两次输入的密码不一致';
      return;
    }
    vaultStatus = await SetupVault(password);
    password = '';
    confirmPassword = '';
    await loadTasks();
  }

  async function unlockVault() {
    authError = '';
    if (!password) {
      authError = '请输入启动密码';
      return;
    }
    try {
      vaultStatus = await UnlockVault(password);
      password = '';
      await loadTasks();
    } catch (error) {
      authError = normalizeError(error);
    }
  }

  async function resetVault() {
    authError = '';
    if (!hasWailsRuntime()) {
      tasks = [];
      vaultStatus = { Initialized: false, Unlocked: false, Path: 'browser-preview' };
      return;
    }
    vaultStatus = await ResetVault();
    tasks = [];
    selectedTask = undefined;
    password = '';
    confirmPassword = '';
  }

  async function loadTasks() {
    if (!hasWailsRuntime()) return;
    tasks = (await ListTasks()) as unknown as GUITask[];
    selectedTask = selectedTask ? tasks.find((task) => task.ID === selectedTask?.ID) : tasks[0];
    startPollingIfNeeded();
  }

  function openNewTask() {
    activeTargetPopoverID = '';
    wizardEditingID = '';
    draftName = '';
    draftDescription = '';
    draftKind = 'single';
    draftRequest = { ...defaults };
    manualTargets = [createManualTarget()];
    fscanPreview = { Total: 0, Targets: [] };
    wizardStep = 1;
    wizardError = '';
    formError = '';
    viewMode = 'wizard';
  }

  function setDraftKind(kind: string) {
    draftKind = (['single', 'fscan', 'sql'].includes(kind) ? kind : 'single') as TaskKind;
  }

  function setDetailTab(tab: string) {
    activeTab = (['hits', 'fields', 'targets', 'sql', 'samples', 'logs'].includes(tab) ? tab : 'hits') as DetailTab;
    if (activeTab === 'sql' && !showSQLTab) activeTab = 'hits';
    if (activeTab !== 'fields') selectedEvidence = undefined;
  }

  function editTask(task: GUITask) {
    activeTargetPopoverID = '';
    wizardEditingID = task.ID;
    draftName = task.Name;
    draftDescription = task.Description;
    draftKind = task.Kind;
    draftRequest = { ...defaults, ...task.Request };
    manualTargets = manualTargetsFromText(draftRequest.FscanText);
    fscanPreview = { Total: 0, Targets: [] };
    wizardStep = 1;
    wizardError = '';
    formError = '';
    viewMode = 'wizard';
  }

  function viewTask(task: GUITask) {
    activeTargetPopoverID = '';
    selectedTask = task;
    activeTab = shouldShowSQLTab(task) && task.Kind === 'sql' ? 'sql' : 'hits';
    selectedEvidence = undefined;
    viewMode = 'detail';
  }

  function nextWizardStep() {
    wizardError = validateWizardStep(wizardStep);
    if (wizardError) return;
    wizardStep = Math.min(4, wizardStep + 1);
  }

  function prevWizardStep() {
    wizardError = '';
    wizardStep = Math.max(1, wizardStep - 1);
  }

  async function saveTask() {
    wizardError = validateWizardStep(4) || validateRequestForKind();
    if (wizardError) return;
    const request = requestForKind();
    try {
      let task: GUITask;
      if (!hasWailsRuntime()) {
        task = {
          ID: wizardEditingID || `preview-${Date.now()}`,
          Name: draftName.trim(),
          Description: draftDescription.trim(),
          Kind: draftKind,
          Status: 'draft',
          Progress: 0,
          Message: '浏览器预览：任务已保存',
          TargetLabel: targetLabel(request, draftKind),
          Request: request,
          State: { ...emptyState, Request: request, Message: '浏览器预览：任务已保存' },
          CreatedAt: new Date().toISOString(),
          UpdatedAt: new Date().toISOString(),
          StartedAt: '',
          FinishedAt: ''
        };
        tasks = wizardEditingID ? tasks.map((item) => (item.ID === wizardEditingID ? task : item)) : [task, ...tasks];
      } else if (wizardEditingID) {
        task = (await UpdateTask({ ID: wizardEditingID, Name: draftName, Description: draftDescription, Kind: draftKind, Request: request } as any)) as unknown as GUITask;
        await loadTasks();
      } else {
        task = (await CreateTask({ Name: draftName, Description: draftDescription, Kind: draftKind, Request: request } as any)) as unknown as GUITask;
        await loadTasks();
      }
      selectedTask = task;
      activeTab = draftKind === 'sql' ? 'sql' : 'hits';
      viewMode = 'detail';
    } catch (error) {
      wizardError = normalizeError(error);
    }
  }

  async function startTask(task = selectedTask) {
    if (!task) return;
    formError = '';
    if (!hasWailsRuntime()) {
      const running = { ...task, Status: 'running', Progress: 42, Message: '浏览器预览：任务运行中', State: demoState('running', task.Request) };
      replaceLocalTask(running);
      selectedTask = running;
      window.setTimeout(() => {
        const completed = { ...running, Status: 'completed', Progress: 100, Message: '浏览器预览：任务完成', State: demoState('completed', task.Request) };
        replaceLocalTask(completed);
        selectedTask = completed;
      }, 900);
      return;
    }
    try {
      const next = (await StartTask(task.ID)) as unknown as GUITask;
      replaceLocalTask(next);
      selectedTask = next;
      startPollingIfNeeded();
    } catch (error) {
      formError = normalizeError(error);
    }
  }

  async function stopTask(task = selectedTask) {
    if (!task) return;
    if (!hasWailsRuntime()) {
      const stopped = { ...task, Status: 'stopped', Message: '浏览器预览：已停止', State: { ...task.State, Status: 'stopped', Message: '浏览器预览：已停止' } };
      replaceLocalTask(stopped);
      selectedTask = stopped;
      return;
    }
    const next = (await StopTask(task.ID)) as unknown as GUITask;
    replaceLocalTask(next);
    selectedTask = next;
  }

  async function deleteTask(task: GUITask) {
    if (!hasWailsRuntime()) {
      tasks = tasks.filter((item) => item.ID !== task.ID);
      if (selectedTask?.ID === task.ID) selectedTask = tasks[0];
      viewMode = 'overview';
      return;
    }
    await DeleteTask(task.ID);
    await loadTasks();
    viewMode = 'overview';
  }

  async function refreshSelectedTask() {
    if (!selectedTask || !hasWailsRuntime()) return;
    const next = (await GetTask(selectedTask.ID)) as unknown as GUITask;
    replaceLocalTask(next);
    selectedTask = next;
  }

  function startPollingIfNeeded() {
    if (!hasWailsRuntime()) return;
    stopPolling();
    if (!tasks.some((task) => task.Status === 'running')) return;
    pollTimer = window.setInterval(async () => {
      const running = tasks.filter((task) => task.Status === 'running');
      for (const task of running) {
        try {
          const next = (await GetTask(task.ID)) as unknown as GUITask;
          replaceLocalTask(next);
          if (selectedTask?.ID === next.ID) selectedTask = next;
        } catch {
          // The task may have been deleted in another window.
        }
      }
      if (!tasks.some((task) => task.Status === 'running')) stopPolling();
    }, 900);
  }

  function stopPolling() {
    if (pollTimer) window.clearInterval(pollTimer);
    pollTimer = undefined;
  }

  function replaceLocalTask(task: GUITask) {
    tasks = tasks.some((item) => item.ID === task.ID) ? tasks.map((item) => (item.ID === task.ID ? task : item)) : [task, ...tasks];
  }

  async function chooseFscanFile() {
    if (!hasWailsRuntime()) {
      draftRequest.Fscan = '/tmp/fscan_result.txt';
      fscanPreview = demoFscanPreview();
      return;
    }
    const path = await ChooseFscanFile();
    if (!path) return;
    draftRequest.Fscan = path;
    draftRequest.FscanText = '';
    fscanPreview = await ParseFscanFile(path);
  }

  async function parseManualTargets() {
    const text = manualTargetsText();
    draftRequest.FscanText = text;
    if (!text) {
      fscanPreview = { Total: 0, Targets: [] };
      return;
    }
    if (!hasWailsRuntime()) {
      fscanPreview = demoFscanPreview(manualTargets.filter(manualTargetHasInput).length);
      return;
    }
    fscanPreview = await ParseFscanText(text);
    draftRequest.Fscan = '';
  }

  async function chooseOutputPath() {
    if (!hasWailsRuntime()) {
      draftRequest.Output = '/tmp/database_scan_report.xlsx';
      return;
    }
    const path = await ChooseOutputPath();
    if (path) draftRequest.Output = path;
  }

  async function openOutput(path: string) {
    if (path && hasWailsRuntime()) await OpenOutputFolder(path);
  }

  function toggleDataManager() {
    showDataManager = !showDataManager;
    backupError = '';
    backupResult = undefined;
  }

  async function chooseBackupExportPath() {
    backupError = '';
    if (!hasWailsRuntime()) {
      backupExportPath = '/tmp/database_scan_backup.dbsbak';
      return;
    }
    const path = await ChooseBackupExportPath();
    if (path) backupExportPath = path;
  }

  async function chooseBackupImportFile() {
    backupError = '';
    if (!hasWailsRuntime()) {
      backupImportPath = '/tmp/database_scan_backup.dbsbak';
      return;
    }
    const path = await ChooseBackupImportFile();
    if (path) backupImportPath = path;
  }

  async function exportBackup() {
    backupError = '';
    backupResult = undefined;
    if (!backupExportPath.trim()) {
      backupError = '请选择或填写备份导出路径';
      return;
    }
    if (backupEncrypt && backupPassword.length < 6) {
      backupError = '加密备份密码至少 6 位';
      return;
    }
    backupBusy = true;
    try {
      if (!hasWailsRuntime()) {
        backupResult = { Path: backupExportPath, Encrypted: backupEncrypt, ExportedTasks: tasks.length, ImportedTasks: 0, RenamedTasks: 0, Message: '浏览器预览：备份导出完成' };
      } else {
        backupResult = await ExportDataBackup({ Path: backupExportPath, Encrypt: backupEncrypt, Password: backupEncrypt ? backupPassword : '' });
      }
    } catch (error) {
      backupError = normalizeError(error);
    } finally {
      backupBusy = false;
    }
  }

  async function importBackup() {
    backupError = '';
    backupResult = undefined;
    if (!backupImportPath.trim()) {
      backupError = '请选择备份文件';
      return;
    }
    backupBusy = true;
    try {
      if (!hasWailsRuntime()) {
        const imported = demoTask('draft');
        imported.ID = `import-${Date.now()}`;
        imported.Name = '导入的数据库扫描任务';
        tasks = [imported, ...tasks];
        backupResult = { Path: backupImportPath, Encrypted: Boolean(backupImportPassword), ExportedTasks: 0, ImportedTasks: 1, RenamedTasks: 0, Message: '浏览器预览：备份导入完成' };
      } else {
        backupResult = await ImportDataBackup({ Path: backupImportPath, Password: backupImportPassword });
        await loadTasks();
      }
    } catch (error) {
      backupError = normalizeError(error);
    } finally {
      backupBusy = false;
    }
  }

  async function testConnectionFromDraft() {
    formError = '';
    connectionTest = undefined;
    const request = { ...draftRequest, Fscan: '', FscanText: '', SQL: '' };
    const error = validateConnection(request);
    if (error) {
      formError = error;
      return;
    }
    testingConnection = true;
    try {
      connectionTest = await callTestConnection(request);
    } catch (error) {
      formError = normalizeError(error);
    } finally {
      testingConnection = false;
    }
  }

  async function testManualTargetConnection(index: number) {
    formError = '';
    connectionTest = undefined;
    const target = manualTargets[index];
    if (!target) return;
    const error = validateManualTarget(target, index + 1);
    if (error) {
      formError = error;
      return;
    }
    const request: ScanRequest = {
      ...defaults,
      Type: target.Type,
      Host: target.Host,
      Port: Number(target.Port),
      User: target.Type === 'redis' ? '' : target.User,
      Password: target.Password,
      Proxy: draftRequest.Proxy,
      Fscan: '',
      FscanText: '',
      SQL: '',
      Output: '',
      SplitOutput: false
    };
    testingConnection = true;
    manualTestingIndex = index;
    try {
      connectionTest = await callTestConnection(request);
    } catch (error) {
      formError = normalizeError(error);
    } finally {
      testingConnection = false;
      manualTestingIndex = -1;
    }
  }

  function requestForKind() {
    const next = { ...draftRequest };
    if (draftKind === 'fscan') {
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
    if (draftKind !== 'sql') next.SQL = '';
    return next;
  }

  function validateWizardStep(step: number) {
    if (step === 1 && !draftName.trim()) return '请先填写任务名称';
    if (step === 3) return validateRequestForKind();
    return '';
  }

  function validateRequestForKind() {
    const next = requestForKind();
    if (draftKind === 'fscan') {
      const manualError = validateManualTargets();
      if (manualError) return manualError;
      if (!next.Fscan.trim() && !next.FscanText.trim()) return '请选择 fscan 结果文件或填写手工目标';
      if (next.SplitOutput && !next.Output.trim()) return '按目标拆分 Excel 需要先填写输出文件路径';
      return '';
    }
    const connectionError = validateConnection(next);
    if (connectionError) return connectionError;
    if (!next.Limit || next.Limit <= 0) return '样例条数必须大于 0';
    if (!next.Workers || next.Workers <= 0) return '并发数必须大于 0';
    if (draftKind === 'sql' && !next.SQL.trim()) return '请填写 SQL 语句';
    return '';
  }

  function validateConnection(next: ScanRequest) {
    if (!next.Type?.trim()) return '请选择数据库类型';
    if (!next.Host?.trim()) return '请填写 Host';
    if (next.Type !== 'redis' && !next.User?.trim()) return '请填写账号；Redis 可留空';
    if (next.Table?.trim() && !next.Database?.trim()) return '指定表时必须同时填写指定库';
    return '';
  }

  function createManualTarget(type = draftRequest.Type || 'mysql'): ManualTarget {
    return {
      ID: manualSeq++,
      Type: type || 'mysql',
      Host: '',
      Port: defaultPorts[type] ?? 3306,
      User: type === 'redis' ? '' : '',
      Password: '',
      ShowPassword: false
    };
  }

  function addManualTarget() {
    const baseType = manualTargets[manualTargets.length - 1]?.Type || draftRequest.Type || 'mysql';
    manualTargets = [...manualTargets, createManualTarget(baseType)];
  }

  function removeLastManualTarget() {
    manualTargets = manualTargets.length <= 1 ? [createManualTarget()] : manualTargets.slice(0, -1);
  }

  function removeManualTarget(index: number) {
    if (manualTargets.length <= 1) {
      manualTargets = [createManualTarget()];
      fscanPreview = { Total: 0, Targets: [] };
      draftRequest.FscanText = '';
      return;
    }
    manualTargets = manualTargets.filter((_, itemIndex) => itemIndex !== index);
  }

  function clearManualTargets() {
    manualTargets = [createManualTarget()];
    fscanPreview = { Total: 0, Targets: [] };
    draftRequest.FscanText = '';
  }

  function changeDraftType() {
    draftRequest.Port = defaultPorts[draftRequest.Type] ?? draftRequest.Port;
    if (draftRequest.Type === 'redis') draftRequest.User = '';
  }

  function changeManualType(index: number) {
    const target = manualTargets[index];
    if (!target) return;
    target.Port = defaultPorts[target.Type] ?? target.Port;
    if (target.Type === 'redis') target.User = '';
    manualTargets = [...manualTargets];
  }

  function updateManualPassword(index: number, event: Event) {
    const target = manualTargets[index];
    const input = event.currentTarget as HTMLInputElement;
    if (!target) return;
    target.Password = input.value;
    manualTargets = [...manualTargets];
  }

  function updateDraftPassword(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    draftRequest.Password = input.value;
  }

  function manualTargetHasInput(target: ManualTarget) {
    return Boolean(target.Host.trim() || target.User.trim() || target.Password.trim());
  }

  function validateManualTargets() {
    const active = manualTargets.filter(manualTargetHasInput);
    for (const [index, target] of active.entries()) {
      const error = validateManualTarget(target, index + 1);
      if (error) return error;
    }
    return '';
  }

  function validateManualTarget(target: ManualTarget, rowNo: number) {
    if (!target.Type) return `第 ${rowNo} 个目标请选择数据库类型`;
    if (!target.Host.trim()) return `第 ${rowNo} 个目标请填写 Host`;
    if (!target.Port || Number(target.Port) <= 0) return `第 ${rowNo} 个目标请填写有效端口`;
    if (target.Type !== 'redis' && !target.User.trim()) return `第 ${rowNo} 个目标请填写账号`;
    return '';
  }

  function manualTargetsText() {
    if (validateManualTargets()) return '';
    return manualTargets.filter(manualTargetHasInput).map(manualLine).join('\n');
  }

  function manualLine(target: ManualTarget) {
    const host = target.Host.trim();
    const port = Number(target.Port);
    if (target.Type === 'redis' && !target.User.trim()) {
      return target.Password.trim() ? `${target.Type} ${host}:${port} ${target.Password.trim()}` : `${target.Type} ${host}:${port}`;
    }
    return `${target.Type} ${host}:${port} ${target.User.trim()}:${target.Password ?? ''}`;
  }

  function manualTargetsFromText(text: string) {
    if (!text.trim()) return [createManualTarget()];
    const targets = parseTargetLines(text).filter((target) => target.Type !== 'fscan 文件');
    if (!targets.length) return [createManualTarget()];
    return targets.map((target) => ({
      ID: manualSeq++,
      Type: target.Type || 'mysql',
      Host: target.Host === '-' ? '' : target.Host,
      Port: Number(target.Port) || defaultPorts[target.Type] || 3306,
      User: target.Type === 'redis' || target.User === '-' ? '' : target.User,
      Password: target.Password || '',
      ShowPassword: false
    }));
  }

  function filterTables(tables: TableResult[], fieldQuery: string, risk: string) {
    const fieldNeedle = fieldQuery.trim().toLowerCase();
    return tables.filter((table) => {
      const fields = table.Fields ?? [];
      const fieldMatch = !fieldNeedle || fields.some((field) => field.Name.toLowerCase().includes(fieldNeedle));
      const riskMatch = risk === 'all' || fields.some((field) => fieldLevel(field) === risk);
      return riskMatch && fieldMatch;
    });
  }

  function sampleGroupsFor(tables: TableResult[], valueQuery: string, metaQuery: string): SampleGroup[] {
    const valueNeedle = valueQuery.trim().toLowerCase();
    const metaNeedle = metaQuery.trim().toLowerCase();
    return tables
      .filter((table) => !metaNeedle || sampleFieldMetaMatches(table, metaNeedle))
      .map((table) => {
        const rows = (table.Rows ?? [])
          .filter((row) => !valueNeedle || Object.values(row.Values ?? {}).some((value) => String(value).toLowerCase().includes(valueNeedle)))
          .map((row) => ({ Values: row.Values ?? {} }));
        return {
          Table: table,
          Headers: sampleGroupHeaders(table, rows),
          Rows: rows
        };
      })
      .filter((group) => group.Rows.length > 0);
  }

  function sampleFieldMetaMatches(table: TableResult, query: string) {
    return (table.Fields ?? []).some((field) => {
      const level = fieldLevel(field);
      const aliases = {
        high: '高敏 高敏感 高危 high',
        medium: '中敏 中敏感 中危 medium',
        low: '低敏 低敏感 低危 low'
      } as Record<string, string>;
      return [
        field.Name,
        (field.Kinds ?? []).join(' '),
        field.Level,
        level,
        levelLabel(level),
        aliases[level] ?? '',
        modeLabel(field.Mode)
      ]
        .join(' ')
        .toLowerCase()
        .includes(query);
    });
  }

  function sampleGroupHeaders(table: TableResult, rows: Array<{ Values: Record<string, string> }>) {
    return Array.from(new Set([...(table.Columns ?? []), ...rows.flatMap((row) => Object.keys(row.Values ?? {}))]));
  }

  function sampleHeaderRisk(table: TableResult, header: string) {
    const normalizedHeader = header.trim().toLowerCase();
    const field = (table.Fields ?? []).find((field) => field.Name.trim().toLowerCase() === normalizedHeader);
    return field ? fieldLevel(field) : '';
  }

  function sampleHeaderKinds(table: TableResult, header: string) {
    const normalizedHeader = header.trim().toLowerCase();
    const field = (table.Fields ?? []).find((field) => field.Name.trim().toLowerCase() === normalizedHeader);
    return field ? (field.Kinds ?? []).join(' / ') : '';
  }

  function evidenceFieldsFor(tables: TableResult[]): EvidenceField[] {
    return tables.flatMap((table) => (table.Fields ?? []).map((field) => ({ ...field, Database: table.Database, TableName: tableLabel(table), Table: table })));
  }

  function selectEvidenceField(field: EvidenceField) {
    selectedEvidence = field;
  }

  function fieldSampleValues(field: EvidenceField) {
    return (field.Table.Rows ?? [])
      .map((row) => sampleValueForField(row, field.Name))
      .filter((value) => value.trim() !== '')
      .map((value, index) => ({ Index: index + 1, Value: value }));
  }

  function sampleValueForField(row: RowSample, fieldName: string) {
    const values = row.Values ?? {};
    if (values[fieldName] !== undefined) return String(values[fieldName]);
    const normalized = fieldName.trim().toLowerCase();
    const key = Object.keys(values).find((item) => item.trim().toLowerCase() === normalized);
    return key ? String(values[key]) : '';
  }

  function riskTotals(tables: TableResult[]) {
    return tables.reduce(
      (acc, table) => {
        for (const field of table.Fields ?? []) acc[fieldLevel(field)] += 1;
        return acc;
      },
      { high: 0, medium: 0, low: 0 } as Record<string, number>
    );
  }

  function taskStats(items: GUITask[]) {
    return {
      total: items.length,
      running: items.filter((task) => task.Status === 'running').length,
      completed: items.filter((task) => task.Status === 'completed').length,
      failed: items.filter((task) => task.Status === 'failed').length
    };
  }

  function shouldShowSQLTab(task?: GUITask) {
    return Boolean(task && (task.Kind === 'sql' || task.State?.SQLResult));
  }

  function detailTabs(includeSQL: boolean): Array<[DetailTab, string]> {
    const tabs: Array<[DetailTab, string]> = [
      ['hits', '命中结果'],
      ['fields', '高危字段'],
      ['targets', '批量目标']
    ];
    if (includeSQL) tabs.push(['sql', 'SQL 结果']);
    tabs.push(['samples', '样例数据'], ['logs', '日志输出']);
    return tabs;
  }

  function targetLabel(request: ScanRequest, kind: TaskKind) {
    if (kind === 'fscan') {
      const total = multiTargetItems(request).filter((target) => target.Type !== 'fscan 文件').length;
      if (total > 0) return `${total} 个目标`;
      if (request.Fscan?.trim()) return '文件导入';
      return '待配置目标';
    }
    return `${request.Type || 'db'}://${request.Host || '未设置'}:${request.Port || '-'}`;
  }

  function taskTargetSummary(task: GUITask) {
    return `${kindLabel(task.Kind)} · ${targetLabel(task.Request, task.Kind)}`;
  }

  function toggleTargetPopover(id: string) {
    activeTargetPopoverID = activeTargetPopoverID === id ? '' : id;
  }

  function multiTargetItems(request: ScanRequest) {
    const lines = parseTargetLines(request.FscanText || '');
    if (lines.length) return lines;
    if (request.Fscan?.trim()) {
      return [{ Index: 1, Type: 'fscan 文件', Host: request.Fscan.trim(), Port: '', User: '-', Password: '', Raw: request.Fscan.trim() }];
    }
    return [];
  }

  function parseTargetLines(text: string) {
    const lines = text.split('\n').map((line) => line.trim()).filter(Boolean);
    const targets = [];
    for (let index = 0; index < lines.length; index++) {
      let parsed = parseTargetLine(lines[index], targets.length);
      if (!parsed && isManualTargetHeader(lines[index]) && lines[index + 1]) {
        parsed = parseTargetLine(`${lines[index]} ${lines[index + 1]}`, targets.length);
        if (parsed) {
          index++;
        }
      }
      if (parsed) {
        targets.push(parsed);
      }
    }
    return targets;
  }

  function isManualTargetHeader(line: string) {
    const parts = line.split(/\s+/);
    return parts.length === 2 && isKnownDBType(parts[0]) && Boolean(splitHostPort(parts[1]));
  }

  function isKnownDBType(value: string) {
    const type = normalizeTargetType(value);
    return Boolean(type && (dbTypes.includes(type) || defaultPorts[type] || type === 'redis'));
  }

  function normalizeTargetType(value: string) {
    const type = value.toLowerCase().replace(/^[\[\]+:]+|[\[\]+:]+$/g, '');
    return type === 'postgresql' ? 'postgres' : type;
  }

  function parseTargetLine(line: string, index: number) {
    const parts = line.split(/\s+/);
    for (let partIndex = 0; partIndex < parts.length; partIndex++) {
      if (isKnownDBType(parts[partIndex])) {
        const type = normalizeTargetType(parts[partIndex]);
        const hostPort = splitHostPort(parts[partIndex + 1] || '');
        if (hostPort) {
          const parsedCredential = splitCredential(parts[partIndex + 2] || '', type);
          if (parsedCredential || type === 'redis') {
            return targetFromParts(index, type, hostPort.host, hostPort.port, parsedCredential?.user || '', parsedCredential?.password || '', line);
          }
        }
        const oldTarget = splitOldHostUser(parts[partIndex + 1] || '');
        if (oldTarget && parts[partIndex + 2] !== undefined) {
          return targetFromParts(index, type, oldTarget.host, oldTarget.port, oldTarget.user, parts[partIndex + 2], line);
        }
      }
      const savedHostPort = splitHostPort(parts[partIndex]);
      if (savedHostPort && isKnownDBType(parts[partIndex + 1] || '')) {
        const type = normalizeTargetType(parts[partIndex + 1]);
        const parsedCredential = splitCredential(parts[partIndex + 2] || '', type);
        if (parsedCredential || type === 'redis') {
          return targetFromParts(index, type, savedHostPort.host, savedHostPort.port, parsedCredential?.user || '', parsedCredential?.password || '', line);
        }
      }
    }
    return null;
  }

  function splitHostPort(value: string) {
    const divider = value.lastIndexOf(':');
    if (divider <= 0 || divider === value.length - 1) return null;
    const host = value.slice(0, divider).replace(/^\[|\]$/g, '');
    const port = value.slice(divider + 1);
    if (!host || !/^\d+$/.test(port)) return null;
    return { host, port };
  }

  function splitCredential(value: string, type: string) {
    if (!value && type === 'redis') return { user: '', password: '' };
    const divider = value.includes(':') ? value.indexOf(':') : value.indexOf('/');
    if (divider < 0) return type === 'redis' && value ? { user: '', password: value } : null;
    const user = value.slice(0, divider);
    const password = value.slice(divider + 1);
    if (!user && type !== 'redis') return null;
    return type === 'redis' && user.toLowerCase() === 'root' ? { user: '', password } : { user, password };
  }

  function splitOldHostUser(value: string) {
    const parts = value.split(':');
    if (parts.length < 3) return null;
    const user = parts[parts.length - 1];
    const port = parts[parts.length - 2];
    const host = parts.slice(0, -2).join(':').replace(/^\[|\]$/g, '');
    if (!host || !user || !/^\d+$/.test(port)) return null;
    return { host, port, user };
  }

  function targetFromParts(index: number, type: string, host: string, port: string, user: string, password: string, raw: string) {
    return { Index: index + 1, Type: type, Host: host || '-', Port: port || '-', User: type === 'redis' ? '-' : user || '-', Password: password || '', Raw: raw };
  }

  function connectionLinesForTask(task: GUITask) {
    if (task.Kind === 'fscan') {
      const targets = multiTargetItems(task.Request).filter((target) => target.Type !== 'fscan 文件');
      if (targets.length) return targets.map(connectionLineFromTarget);
    }
    return [connectionLineFromRequest(task.Request)];
  }

  function connectionLineFromTarget(target: { Type: string; Host: string; Port: string; User: string; Password: string }) {
    const auth = target.Type === 'redis' ? target.Password || '-' : `${target.User || '-'}/${target.Password || '-'}`;
    return `${target.Type} ${target.Host}:${target.Port || '-'} ${auth}`;
  }

  function connectionLineFromRequest(request: ScanRequest) {
    const type = request.Type || 'db';
    const host = request.Host || '-';
    const port = request.Port || '-';
    const auth = type === 'redis' ? request.Password || '-' : `${request.User || '-'}/${request.Password || '-'}`;
    return `${type} ${host}:${port} ${auth}`;
  }

  async function copyTaskConnections(task: GUITask) {
    const text = connectionLinesForTask(task).join('\n');
    await copyText(text);
    copiedTaskID = task.ID;
    if (copyTimer) window.clearTimeout(copyTimer);
    copyTimer = window.setTimeout(() => {
      if (copiedTaskID === task.ID) copiedTaskID = '';
    }, 1600);
  }

  async function copyText(text: string) {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.setAttribute('readonly', 'true');
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    document.execCommand('copy');
    textarea.remove();
  }

  function proxyLabel(request: ScanRequest) {
    const proxy = request.Proxy?.trim();
    return proxy ? `代理 ${proxy}` : '代理 直连';
  }

  function tableLabel(table: TableResult) {
    return table.Schema ? `${table.Schema}.${table.Name}` : table.Name;
  }

  function statusLabel(status: string) {
    return ({ draft: '待配置', idle: '待扫描', running: '运行中', completed: '已完成', stopped: '已停止', failed: '失败' } as Record<string, string>)[status] ?? status;
  }

  function kindLabel(kind: string) {
    return ({ single: '单目标扫描', fscan: '多目标加载', sql: 'SQL 语句执行' } as Record<string, string>)[kind] ?? kind;
  }

  function modeLabel(mode: string) {
    return ({ 'field-content': '字段名+内容', 'field-name': '字段名', content: '内容正则', all: '全部' } as Record<string, string>)[mode] ?? mode;
  }

  function levelLabel(level: string) {
    return ({ high: '高敏', medium: '中敏', low: '低敏', all: '全部' } as Record<string, string>)[level] ?? level;
  }

  function encodingLabel(encoding: string) {
    return textEncodingOptions.find(([value]) => value === (encoding || 'auto'))?.[1] ?? encoding;
  }

  function fieldLevel(field: FieldResult) {
    if (field.Level && field.Level !== 'all') return field.Level;
    const kinds = (field.Kinds ?? []).join('/');
    if (kinds.includes('密码') || kinds.includes('身份证') || kinds.includes('银行卡')) return 'high';
    if (kinds.includes('手机号') || kinds.includes('邮箱')) return 'medium';
    return 'low';
  }

  function normalizeDefaults(next: ScanRequest) {
    if (!next.Host) next.Host = fallbackDefaults.Host;
    if (!next.Port) next.Port = fallbackDefaults.Port;
    if (!next.Limit) next.Limit = fallbackDefaults.Limit;
    if (!next.Workers) next.Workers = fallbackDefaults.Workers;
    if (!next.Timeout) next.Timeout = fallbackDefaults.Timeout;
    return next;
  }

  function normalizeError(error: unknown) {
    const message = String(error).replace(/^Error:\s*/, '');
    if (message.includes('password is incorrect')) return '启动密码不正确，或本地任务库已损坏';
    if (message.includes('type, host and user are required')) return '请检查数据库类型、Host 和账号是否已填写；Redis 账号可以留空。';
    if (message.includes('sql is required')) return '请填写 SQL 语句。';
    return message;
  }

  function formatTime(value: string) {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
  }

  function demoTask(status: string): GUITask {
    const fscanText = [
      'mysql 127.0.0.1:3306 root:demo',
      'postgres 127.0.0.1:5432 audit:demo',
      'redis 127.0.0.1:6379 demo',
      'mssql 127.0.0.1:1433 sa:demo',
      'oracle 127.0.0.1:1521 system:demo'
    ].join('\n');
    const request = { ...fallbackDefaults, User: 'root', Password: 'encrypted-demo', Database: 'audit_lab', FscanText: status === 'completed' ? '' : fscanText };
    const kind: TaskKind = status === 'completed' ? 'single' : 'fscan';
    return {
      ID: `demo-${status}`,
      Name: status === 'completed' ? '客户数据敏感字段审计' : '新建数据库扫描任务',
      Description: status === 'completed' ? '验证 access_tokens 与 customer_profile 中的密码、手机号、邮箱字段。' : '填写任务详情后设置扫描目标。',
      Kind: kind,
      Status: status,
      Progress: status === 'completed' ? 100 : 0,
      Message: status === 'completed' ? '浏览器预览：任务完成' : '等待配置目标',
      TargetLabel: targetLabel(request, kind),
      Request: request,
      State: demoState(status, request),
      CreatedAt: new Date().toISOString(),
      UpdatedAt: new Date().toISOString(),
      StartedAt: '',
      FinishedAt: ''
    };
  }

  function demoState(status: string, request: ScanRequest): ScanState {
    const completed = status === 'completed';
    return {
      ...emptyState,
      JobID: `demo-${status}`,
      Status: status,
      Progress: completed ? 100 : 42,
      Message: completed ? '浏览器预览：扫描完成' : '浏览器预览：扫描中',
      TargetLabel: targetLabel(request, 'single'),
      Request: request,
      Outputs: completed ? ['/tmp/database_scan_report.xlsx'] : [],
      Logs: [
        { Time: '10:01:03', Level: 'info', Message: '任务已提交扫描引擎' },
        { Time: '10:01:05', Level: 'info', Message: '发现 access_tokens 命中字段' }
      ],
      Result: {
        Errors: [],
        Tables: completed
          ? [
              {
                Database: 'audit_lab',
                Schema: '',
                Name: 'access_tokens',
                Total: 2,
                Columns: ['id', 'service_name', 'secret_key', 'refresh_token', 'owner_email'],
                Fields: [
                  { Name: 'secret_key', Kinds: ['密码/密钥'], Level: 'high', Mode: 'field-content', Total: 2 },
                  { Name: 'refresh_token', Kinds: ['token'], Level: 'high', Mode: 'field-content', Total: 2 },
                  { Name: 'owner_email', Kinds: ['邮箱'], Level: 'medium', Mode: 'field-content', Total: 2 }
                ],
                Rows: [
                  { Values: { id: '1', service_name: 'billing', secret_key: 'sk_live_demo', refresh_token: 'rt_mysql_refresh', owner_email: 'ops@example.internal' } },
                  { Values: { id: '2', service_name: 'crm', secret_key: 'sk_crm_demo', refresh_token: 'rt_crm_refresh', owner_email: 'sec@example.internal' } }
                ]
              }
            ]
          : []
      },
      SQLResult: request.SQL
        ? { Columns: ['id', 'email'], Rows: [['1', 'ops@example.internal']], Total: 1, Shown: 1, Affected: 0, IsQuery: true }
        : undefined
    };
  }

  function demoFscanPreview(total = 2): FscanPreview {
    return {
      Total: total,
      Targets: [
        { Type: 'mysql', Host: '10.211.55.16', Port: 3306, User: 'root', Line: 1, Raw: 'mysql 10.211.55.16:3306 root:pass', Password: true },
        { Type: 'redis', Host: '10.211.55.17', Port: 6379, User: '', Line: 2, Raw: 'redis 10.211.55.17:6379 pass', Password: true }
      ].slice(0, Math.max(1, total))
    };
  }

  function demoConnection(request: ScanRequest): ConnectionTestResult {
    return {
      Success: true,
      Message: '浏览器预览：连接参数格式通过',
      Type: request.Type,
      Host: request.Host,
      Port: Number(request.Port),
      Database: request.Database,
      User: request.User,
      Proxy: request.Proxy,
      Version: 'preview',
      ResolvedAddr: `${request.Host}:${request.Port}`,
      ServerTime: new Date().toLocaleString()
    };
  }

</script>

{#if loading}
  <main class="auth-shell">
    <section class="auth-card">
      <div class="brand-lock">DB</div>
      <h1>Database Scan</h1>
      <p>正在载入任务工作台</p>
    </section>
  </main>
{:else if !vaultStatus.Unlocked}
  <main class="auth-shell">
    <section class="auth-card">
      <div class="brand-lock">DB</div>
      <label class="theme-select auth-theme">
        <span>主题</span>
        <select bind:value={themePreference} on:change={handleThemeChange}>
          <option value="system">跟随系统</option>
          <option value="light">白色</option>
          <option value="dark">黑色</option>
        </select>
      </label>
      <h1>{vaultStatus.Initialized ? '解锁任务库' : '设置启动密码'}</h1>
      <p>任务配置、数据库密码和扫描结果会写入本地 SQLCipher 加密数据库。</p>
      <label>
        <span>启动密码</span>
        <input type="password" bind:value={password} placeholder="输入启动密码" on:keydown={(event) => event.key === 'Enter' && (vaultStatus.Initialized ? unlockVault() : setupVault())} />
      </label>
      {#if !vaultStatus.Initialized}
        <label>
          <span>确认密码</span>
          <input type="password" bind:value={confirmPassword} placeholder="再次输入启动密码" on:keydown={(event) => event.key === 'Enter' && setupVault()} />
        </label>
      {/if}
      {#if authError}<p class="error-line">{authError}</p>{/if}
      <div class="auth-actions">
        {#if vaultStatus.Initialized}
          <button class="primary" on:click={unlockVault}>解锁</button>
          <button on:click={resetVault}>清空并重置</button>
        {:else}
          <button class="primary" on:click={setupVault}>创建加密任务库</button>
        {/if}
      </div>
    </section>
  </main>
{:else}
  <main class="platform-shell">
    <header class="app-topbar">
      <div class="brand-block">
        <div class="brand-logo">DB</div>
        <div>
          <h1>Database Scan</h1>
          <p>攻击面数据审计平台</p>
        </div>
      </div>
      <label class="search-box" aria-label="搜索任务">
        <input bind:value={taskSearch} placeholder="搜索任务名称、详情或目标" />
      </label>
      <div class="topbar-actions">
        <label class="theme-select">
          <span>主题</span>
          <select bind:value={themePreference} on:change={handleThemeChange}>
            <option value="system">跟随系统</option>
            <option value="light">白色</option>
            <option value="dark">黑色</option>
          </select>
        </label>
        <button on:click={toggleDataManager}>数据管理</button>
        <button class="primary" on:click={openNewTask}>新建任务</button>
      </div>
    </header>

    {#if viewMode === 'overview'}
      <section class="workspace overview-workspace">
        <div class="hero-panel">
          <div>
            <span>任务工作台</span>
            <h2>新建任务后再设置扫描目标，结果进入任务详情查看。</h2>
            <p>当前任务库：{vaultStatus.Path || 'browser-preview'}</p>
          </div>
          <div class="hero-actions">
            <button on:click={toggleDataManager}>数据管理</button>
            <button class="primary" on:click={openNewTask}>新建任务</button>
          </div>
        </div>
        {#if showDataManager}
          <section class="data-manager">
            <div class="section-heading">
              <h2>数据管理</h2>
              <span>SQLCipher 本地加密数据库</span>
            </div>
            <div class="vault-path">
              <span>当前数据库路径</span>
              <code>{vaultStatus.Path || 'browser-preview'}</code>
            </div>
            <div class="backup-grid">
              <section class="backup-card">
                <div>
                  <h2>导出备份</h2>
                  <p>导出任务、配置、扫描结果、日志和输出文件记录，不打包 Excel 文件本体。</p>
                </div>
                <label>
                  <span>备份文件</span>
                  <div class="inline-controls">
                    <input bind:value={backupExportPath} placeholder="/tmp/database_scan_backup.dbsbak" />
                    <button on:click={chooseBackupExportPath}>选择位置</button>
                  </div>
                </label>
                <label class="inline-check">
                  <input type="checkbox" bind:checked={backupEncrypt} />
                  <span>加密备份</span>
                </label>
                {#if backupEncrypt}
                  <label>
                    <span>备份密码</span>
                    <input type="password" bind:value={backupPassword} placeholder="独立备份密码，至少 6 位" />
                  </label>
                {:else}
                  <p class="warning-line">不加密备份会暴露本地任务、数据库连接信息和扫描结果。</p>
                {/if}
                <button class="primary" on:click={exportBackup} disabled={backupBusy}>{backupBusy ? '处理中' : '导出备份'}</button>
              </section>
              <section class="backup-card">
                <div>
                  <h2>导入备份</h2>
                  <p>默认合并导入。ID 冲突会自动生成新 ID，任务名重复会自动重命名。</p>
                </div>
                <label>
                  <span>备份文件</span>
                  <div class="inline-controls">
                    <input bind:value={backupImportPath} placeholder="/tmp/database_scan_backup.dbsbak" />
                    <button on:click={chooseBackupImportFile}>选择文件</button>
                  </div>
                </label>
                <label>
                  <span>备份密码</span>
                  <input type="password" bind:value={backupImportPassword} placeholder="加密备份请输入密码，明文备份可留空" />
                </label>
                <button on:click={importBackup} disabled={backupBusy}>{backupBusy ? '处理中' : '导入备份'}</button>
              </section>
            </div>
            {#if backupError}<p class="error-line">{backupError}</p>{/if}
            {#if backupResult}
              <div class="backup-result">
                <strong>{backupResult.Message}</strong>
                <span>{backupResult.Path}</span>
                {#if backupResult.ExportedTasks}<em>导出任务 {backupResult.ExportedTasks}</em>{/if}
                {#if backupResult.ImportedTasks}<em>导入任务 {backupResult.ImportedTasks}，重命名 {backupResult.RenamedTasks}</em>{/if}
                <em>{backupResult.Encrypted ? '加密备份' : '明文备份'}</em>
              </div>
            {/if}
          </section>
        {/if}
        <div class="stat-grid">
          <div><span>全部任务</span><strong>{boardStats.total}</strong></div>
          <div><span>运行中</span><strong>{boardStats.running}</strong></div>
          <div><span>已完成</span><strong>{boardStats.completed}</strong></div>
          <div><span>失败</span><strong>{boardStats.failed}</strong></div>
        </div>
        <section class="task-list">
          <div class="section-heading">
            <h2>任务列表</h2>
            <span>{filteredTasks.length} tasks</span>
          </div>
          {#if filteredTasks.length === 0}
            <div class="empty-state">
              <strong>还没有任务</strong>
              <span>创建一个任务，填写名称与详情后进入扫描目标配置。</span>
              <button class="primary" on:click={openNewTask}>新建任务</button>
            </div>
          {:else}
            {#each filteredTasks as task (task.ID)}
              <article class="task-row">
                <div class="task-main">
                  <strong>{task.Name}</strong>
                  <span>{task.Description || '未填写任务详情'}</span>
                </div>
                <span class:running={task.Status === 'running'} class:completed={task.Status === 'completed'} class:failed={task.Status === 'failed'} class="status-pill">{statusLabel(task.Status)}</span>
                <div class="task-progress">
                  {#if task.Kind === 'fscan'}
                    <button class="target-summary-button" on:click={() => toggleTargetPopover(task.ID)}>{taskTargetSummary(task)}</button>
                    {#if activeTargetPopoverID === task.ID}
                      <aside class="target-popover">
                        <div class="target-popover-head">
                          <strong>目标详情</strong>
                          <button on:click={() => (activeTargetPopoverID = '')}>关闭</button>
                        </div>
                        {#each multiTargetItems(task.Request) as target}
                          <div class="target-popover-row">
                            <span>{target.Index}</span>
                            <strong>{target.Type}</strong>
                            <em>{target.Host}{target.Port ? `:${target.Port}` : ''}</em>
                            <code>{target.User}</code>
                          </div>
                        {:else}
                          <p>暂无目标详情，进入配置页加载 fscan 文件或手工目标。</p>
                        {/each}
                      </aside>
                    {/if}
                  {:else}
                    <span>{taskTargetSummary(task)}</span>
                  {/if}
                  <span class="proxy-line">{proxyLabel(task.Request)}</span>
                  <div class="task-risk-row">
                    {#if riskTotals(task.State?.Result?.Tables ?? []).high || riskTotals(task.State?.Result?.Tables ?? []).medium || riskTotals(task.State?.Result?.Tables ?? []).low}
                      <i class="risk-pill high">高 {riskTotals(task.State?.Result?.Tables ?? []).high}</i>
                      <i class="risk-pill medium">中 {riskTotals(task.State?.Result?.Tables ?? []).medium}</i>
                      <i class="risk-pill low">低 {riskTotals(task.State?.Result?.Tables ?? []).low}</i>
                    {:else}
                      <i class="risk-pill empty">暂无风险数据</i>
                    {/if}
                  </div>
                  <div class="progress-line"><i style={`width: ${Math.max(0, Math.min(100, task.Progress || task.State?.Progress || 0))}%`}></i></div>
                </div>
                <div class="task-actions">
                  <button class:copied={copiedTaskID === task.ID} on:click|stopPropagation={() => copyTaskConnections(task)}>{copiedTaskID === task.ID ? '已复制' : '复制'}</button>
                  <button on:click={() => editTask(task)} disabled={task.Status === 'running'}>配置</button>
                  <button on:click={() => viewTask(task)}>详情</button>
                  <button class="primary" on:click={() => startTask(task)} disabled={task.Status === 'running'}>开始</button>
                </div>
              </article>
            {/each}
          {/if}
        </section>
      </section>
    {:else if viewMode === 'wizard'}
      <section class="workspace wizard-workspace">
        <div class="wizard-header">
          <button on:click={() => (viewMode = 'overview')}>返回任务列表</button>
          <div>
            <h2>{wizardEditingID ? '编辑任务' : '新建任务'}</h2>
            <span>先定义任务，再设置扫描目标</span>
          </div>
        </div>
        <div class="stepper">
          {#each ['任务信息', '任务类型', '扫描目标', '扫描参数'] as label, index}
            <button class:active={wizardStep === index + 1} class:done={wizardStep > index + 1} on:click={() => (wizardStep = index + 1)}>{index + 1}. {label}</button>
          {/each}
        </div>

        <section class="setup-card">
          {#if wizardStep === 1}
            <div class="card-title">
              <h2>任务名称与详情</h2>
              <span>名称会显示在任务列表，详情用于说明扫描目的。</span>
            </div>
            <div class="form-grid two">
              <label>
                <span>任务名称</span>
                <input bind:value={draftName} placeholder="例如：生产客户库敏感字段审计" />
              </label>
              <label>
                <span>任务详情</span>
                <textarea bind:value={draftDescription} placeholder="记录扫描范围、授权背景、关注字段或交付说明"></textarea>
              </label>
            </div>
          {:else if wizardStep === 2}
            <div class="card-title">
              <h2>选择任务类型</h2>
              <span>不同类型会进入不同的目标配置方式。</span>
            </div>
            <div class="kind-grid">
              {#each [
                { id: 'single', title: '单目标扫描', body: '连接一个数据库实例并扫描敏感字段。' },
                { id: 'fscan', title: '多目标加载', body: '导入 fscan 结果或手工维护多个目标。' },
                { id: 'sql', title: 'SQL 语句执行', body: '连接数据库后执行自定义 SQL。' }
              ] as option}
                <button class:active={draftKind === option.id} on:click={() => setDraftKind(option.id)}>
                  <strong>{option.title}</strong>
                  <span>{option.body}</span>
                </button>
              {/each}
            </div>
          {:else if wizardStep === 3}
            <div class="card-title">
              <h2>设置扫描目标</h2>
              <span>{kindLabel(draftKind)}</span>
            </div>
            {#if draftKind === 'fscan'}
              <div class="fscan-layout">
                <label>
                  <span>fscan 结果文件</span>
                  <div class="inline-controls">
                    <input bind:value={draftRequest.Fscan} placeholder="/tmp/fscan_result.txt" />
                    <button on:click={chooseFscanFile}>选择文件</button>
                  </div>
                </label>
                <label class="batch-proxy">
                  <span>批量代理</span>
                  <input bind:value={draftRequest.Proxy} placeholder="socks5://127.0.0.1:1080，留空则直连" />
                </label>
                <div class="manual-toolbar">
                  <h2>手工多目标</h2>
                  <div>
                    <button on:click={addManualTarget}>新增目标</button>
                    <button on:click={removeLastManualTarget}>删除末项</button>
                    <button on:click={clearManualTargets}>清空</button>
                    <button class="primary" on:click={parseManualTargets}>解析预览</button>
                  </div>
                </div>
                <div class="manual-table">
                  {#each manualTargets as target, index (target.ID)}
                    <div class="manual-row">
                      <select bind:value={target.Type} on:change={() => changeManualType(index)}>
                        {#each dbTypes as type}<option value={type}>{type}</option>{/each}
                      </select>
                      <input bind:value={target.Host} placeholder="Host" />
                      <input type="number" bind:value={target.Port} placeholder="端口" />
                      <input bind:value={target.User} placeholder="账号" disabled={target.Type === 'redis'} />
                      <input type={target.ShowPassword ? 'text' : 'password'} value={target.Password} on:input={(event) => updateManualPassword(index, event)} placeholder="密码" />
                      <button on:click={() => ((target.ShowPassword = !target.ShowPassword), (manualTargets = [...manualTargets]))}>{target.ShowPassword ? '隐藏' : '查看'}</button>
                      <button on:click={() => testManualTargetConnection(index)} disabled={testingConnection && manualTestingIndex === index}>{testingConnection && manualTestingIndex === index ? '测试中' : '测试'}</button>
                      <button class="danger" on:click={() => removeManualTarget(index)}>删除</button>
                    </div>
                  {/each}
                </div>
                {#if connectionTest}
                  <div class="manual-test-result" class:ok={connectionTest.Success}>
                    <strong>{connectionTest.Success ? '连接通过' : '连接失败'}</strong>
                    <span>{connectionTest.Message} · {connectionTest.Type}://{connectionTest.Host}:{connectionTest.Port}{connectionTest.Proxy ? ` · ${connectionTest.Proxy}` : ''}</span>
                  </div>
                {/if}
                <div class="preview-box">
                  <strong>fscan 解析结果</strong>
                  <span>{fscanPreview.Total || 0} 个目标</span>
                  {#each fscanPreview.Targets ?? [] as target}
                    <code>{target.Type} {target.Host}:{target.Port} {target.User || '-'}</code>
                  {/each}
                </div>
              </div>
            {:else}
              <div class="form-grid">
                <label>
                  <span>数据库类型</span>
                  <select bind:value={draftRequest.Type} on:change={changeDraftType}>
                    {#each dbTypes as type}<option value={type}>{type}</option>{/each}
                  </select>
                </label>
                <label>
                  <span>Host</span>
                  <input bind:value={draftRequest.Host} placeholder="127.0.0.1" />
                </label>
                <label>
                  <span>端口</span>
                  <input type="number" bind:value={draftRequest.Port} />
                </label>
                <label>
                  <span>账号</span>
                  <input bind:value={draftRequest.User} disabled={draftRequest.Type === 'redis'} />
                </label>
                <label>
                  <span>密码</span>
                  <div class="inline-controls">
                    <input type={showPassword ? 'text' : 'password'} value={draftRequest.Password} on:input={updateDraftPassword} />
                    <button on:click={() => (showPassword = !showPassword)}>{showPassword ? '隐藏' : '查看'}</button>
                  </div>
                </label>
                <label>
                  <span>指定库</span>
                  <input bind:value={draftRequest.Database} placeholder="audit_lab" />
                </label>
                <label>
                  <span>指定表</span>
                  <input bind:value={draftRequest.Table} placeholder="schema.table" />
                </label>
                <label>
                  <span>代理</span>
                  <input bind:value={draftRequest.Proxy} placeholder="socks5://127.0.0.1:1080" />
                </label>
              </div>
              {#if draftKind === 'sql'}
                <label class="sql-editor">
                  <span>SQL 语句</span>
                  <textarea bind:value={draftRequest.SQL} placeholder="select * from users limit 20"></textarea>
                </label>
              {/if}
              <div class="form-actions">
                <button on:click={testConnectionFromDraft} disabled={testingConnection}>{testingConnection ? '测试中' : '测试连接'}</button>
                {#if connectionTest}
                  <span class:ok={connectionTest.Success} class="connection-note">{connectionTest.Message} · {connectionTest.ResolvedAddr}</span>
                {/if}
              </div>
            {/if}
          {:else}
            <div class="card-title">
              <h2>扫描参数</h2>
              <span>保存后会进入任务详情，可以立即启动。</span>
            </div>
            <div class="form-grid params-grid">
              <label>
                <span>模式</span>
                <select bind:value={draftRequest.Mode}>
                  <option value="field-content">字段名+内容</option>
                  <option value="field-name">字段名</option>
                  <option value="content">内容正则</option>
                  <option value="all">全部</option>
                </select>
              </label>
              <label>
                <span>敏感级别</span>
                <select bind:value={draftRequest.Level}>
                  <option value="all">全部</option>
                  <option value="high">高敏</option>
                  <option value="medium">中敏</option>
                  <option value="low">低敏</option>
                </select>
              </label>
              <label>
                <span>样例条数</span>
                <input type="number" min="1" bind:value={draftRequest.Limit} />
              </label>
              <label>
                <span>并发</span>
                <input type="number" min="1" bind:value={draftRequest.Workers} />
              </label>
              <label>
                <span>超时</span>
                <input bind:value={draftRequest.Timeout} />
              </label>
              <label>
                <span>内容编码</span>
                <select bind:value={draftRequest.TextEncoding}>
                  {#each textEncodingOptions as [value, label]}<option value={value}>{label}</option>{/each}
                </select>
              </label>
              <label>
                <span>Excel 输出</span>
                <div class="inline-controls">
                  <input bind:value={draftRequest.Output} placeholder="/tmp/database_scan_report.xlsx" />
                  <button on:click={chooseOutputPath}>选择输出</button>
                </div>
              </label>
            </div>
            <div class="check-row">
              <label><input type="checkbox" bind:checked={draftRequest.IncludeSystem} /> 系统库</label>
              <label><input type="checkbox" bind:checked={draftRequest.Mask} /> 脱敏</label>
              {#if draftKind === 'fscan'}<label><input type="checkbox" bind:checked={draftRequest.SplitOutput} /> 按目标拆分 Excel</label>{/if}
            </div>
            <div class="summary-strip">
              <strong>{draftName || '未命名任务'}</strong>
              <span>{kindLabel(draftKind)} · {modeLabel(draftRequest.Mode)} / {levelLabel(draftRequest.Level)} / {encodingLabel(draftRequest.TextEncoding)}</span>
              <span>{targetLabel(requestForKind(), draftKind)}</span>
            </div>
          {/if}
        </section>

        {#if wizardError || formError}<p class="error-line">{wizardError || formError}</p>{/if}
        <div class="wizard-actions">
          <button on:click={prevWizardStep} disabled={wizardStep === 1}>上一步</button>
          {#if wizardStep < 4}
            <button class="primary" on:click={nextWizardStep}>下一步</button>
          {:else}
            <button class="primary" on:click={saveTask}>保存并进入详情</button>
          {/if}
        </div>
      </section>
    {:else if selectedTask}
      <section class="workspace detail-workspace">
        <div class="detail-titlebar">
          <button on:click={() => (viewMode = 'overview')}>返回任务列表</button>
          <div class="detail-title">
            <span>{kindLabel(selectedTask.Kind)} · {statusLabel(selectedTask.Status)}</span>
            <h2>{selectedTask.Name}</h2>
            <p>{selectedTask.Description || '未填写任务详情'}</p>
          </div>
          <div class="detail-actions">
            <button class:copied={copiedTaskID === selectedTask.ID} on:click={() => copyTaskConnections(selectedTask)}>{copiedTaskID === selectedTask.ID ? '已复制' : '复制连接'}</button>
            <button on:click={() => editTask(selectedTask)} disabled={selectedTask.Status === 'running'}>配置</button>
            {#if selectedTask.Status === 'running'}
              <button on:click={() => stopTask()}>停止</button>
            {:else}
              <button class="primary" on:click={() => startTask()}>开始</button>
            {/if}
            <button on:click={refreshSelectedTask}>刷新</button>
            <button class="danger" on:click={() => deleteTask(selectedTask)}>删除</button>
          </div>
        </div>
        {#if formError}<p class="error-line">{formError}</p>{/if}
        <div class="detail-grid">
          <section class="overview-panel">
            <div class="metric-row">
              <div><span>命中表</span><strong>{currentTables.length}</strong></div>
              <div><span>敏感字段</span><strong>{currentEvidence.length}</strong></div>
              <div><span>高敏命中</span><strong>{currentRisk.high}</strong></div>
              <div><span>当前进度</span><strong>{currentState.Progress || selectedTask.Progress || 0}%</strong></div>
            </div>
            <div class="overview-risk-row">
              <span class="risk-pill high">高危 {currentRisk.high}</span>
              <span class="risk-pill medium">中危 {currentRisk.medium}</span>
              <span class="risk-pill low">低危 {currentRisk.low}</span>
            </div>
            <div class="progress-line"><i style={`width: ${Math.max(0, Math.min(100, currentState.Progress || selectedTask.Progress || 0))}%`}></i></div>
            <div class="overview-foot">
              <span>{currentState.Message || selectedTask.Message || '等待扫描'}</span>
              <em>{currentState.TargetLabel || selectedTask.TargetLabel || targetLabel(selectedTask.Request, selectedTask.Kind)}</em>
            </div>
          </section>
          <section class="task-info-panel">
            <h2>任务信息</h2>
            <div><span>创建时间</span><strong>{formatTime(selectedTask.CreatedAt)}</strong></div>
            <div><span>更新时间</span><strong>{formatTime(selectedTask.UpdatedAt)}</strong></div>
            <div><span>扫描模式</span><strong>{modeLabel(selectedTask.Request.Mode)}</strong></div>
            <div><span>内容编码</span><strong>{encodingLabel(selectedTask.Request.TextEncoding)}</strong></div>
            <div><span>代理</span><strong>{proxyLabel(selectedTask.Request).replace(/^代理\s*/, '')}</strong></div>
            <div class="output-info">
              <span>输出文件</span>
              <strong>{outputPath || '-'}</strong>
              {#if outputPath}<button on:click={() => openOutput(outputPath)}>打开目录</button>{/if}
            </div>
          </section>
        </div>

        <section class="result-panel" class:has-field-popover={activeTab === 'fields' && Boolean(selectedEvidence)}>
          <div class="activity-tabs">
            {#each detailTabs(showSQLTab) as tab}
              <button class:active={activeTab === tab[0]} on:click={() => setDetailTab(tab[0])}>{tab[1]}</button>
            {/each}
          </div>

          {#if activeTab === 'hits'}
            <div class="table-tools">
              <label><span>字段检索</span><input bind:value={fieldQuery} placeholder="phone / id_card / token" /></label>
              <label><span>风险</span><select bind:value={riskFilter}><option value="all">全部</option><option value="high">高危</option><option value="medium">中危</option><option value="low">低危</option></select></label>
            </div>
            <div class="result-table-wrap">
              <table>
                <thead><tr><th>数据库</th><th>表</th><th>敏感字段</th><th>存在行数</th><th>风险</th></tr></thead>
                <tbody>
                  {#each filteredTables as table}
                    <tr>
                      <td>{table.Database}</td>
                      <td>{tableLabel(table)}</td>
                      <td>{(table.Fields ?? []).map((field) => field.Name).join(', ')}</td>
                      <td>{table.Total}</td>
                      <td><span class={`risk-dot ${table.Fields?.some((field) => fieldLevel(field) === 'high') ? 'high' : 'medium'}`}>{table.Fields?.some((field) => fieldLevel(field) === 'high') ? '高敏' : '命中'}</span></td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else if activeTab === 'fields'}
            <div class="field-list" class:has-popover={Boolean(selectedEvidence)}>
              {#each currentEvidence as field}
                <div class="field-card" class:active={selectedEvidence?.Name === field.Name && selectedEvidence?.Database === field.Database && selectedEvidence?.TableName === field.TableName}>
                  <button class={`field-row ${fieldLevel(field)}`} class:selected={selectedEvidence?.Name === field.Name && selectedEvidence?.Database === field.Database && selectedEvidence?.TableName === field.TableName} on:click={() => selectEvidenceField(field)}>
                    <strong>{field.Name}</strong>
                    <span>{field.Database} / {field.TableName}</span>
                    <em>{(field.Kinds ?? []).join(' / ')} · {levelLabel(fieldLevel(field))}</em>
                    <small>命中行数 {field.Total || field.Table.Total || 0}</small>
                  </button>
                  {#if selectedEvidence?.Name === field.Name && selectedEvidence?.Database === field.Database && selectedEvidence?.TableName === field.TableName}
                    <aside class={`field-popover ${fieldLevel(selectedEvidence)}`}>
                      <header>
                        <div>
                          <span>字段详情</span>
                          <strong>{selectedEvidence.Name}</strong>
                        </div>
                        <button type="button" on:click={() => (selectedEvidence = undefined)}>关闭</button>
                      </header>
                      <div class="field-detail-grid">
                        <div><span>数据库</span><strong>{selectedEvidence.Database}</strong></div>
                        <div><span>表</span><strong>{selectedEvidence.TableName}</strong></div>
                        <div><span>风险等级</span><strong>{levelLabel(fieldLevel(selectedEvidence))}</strong></div>
                        <div><span>命中数量</span><strong>{selectedEvidence.Total || selectedEvidence.Table.Total || 0}</strong></div>
                        <div><span>命中类型</span><strong>{(selectedEvidence.Kinds ?? []).join(' / ') || '-'}</strong></div>
                        <div><span>扫描模式</span><strong>{modeLabel(selectedEvidence.Mode)}</strong></div>
                      </div>
                      <section class="field-samples">
                        <div class="field-samples-heading">
                          <span>样例值</span>
                          <strong>{fieldSampleValues(selectedEvidence).length} values</strong>
                        </div>
                        {#each fieldSampleValues(selectedEvidence) as sample}
                          <code><span>#{sample.Index}</span>{sample.Value}</code>
                        {:else}
                          <p>暂无样例值</p>
                        {/each}
                      </section>
                    </aside>
                  {/if}
                </div>
              {/each}
            </div>
          {:else if activeTab === 'targets'}
            <div class="target-list">
              {#if selectedTask.Kind === 'fscan'}
                {#each (selectedTask.Request.FscanText || '').split('\n').filter(Boolean) as line, index}
                  <code>{index + 1}. {line}</code>
                {:else}
                  <code>{selectedTask.Request.Fscan || '暂无批量目标，返回配置页导入 fscan 文件或填写手工目标。'}</code>
                {/each}
              {:else}
                <code>{targetLabel(selectedTask.Request, selectedTask.Kind)}</code>
              {/if}
            </div>
          {:else if activeTab === 'sql'}
            <div class="sql-panel">
              {#if currentState.SQLResult}
                <div class="section-heading">
                  <h2>SQL 执行结果</h2>
                  <span>{currentState.SQLResult.Shown}/{currentState.SQLResult.Total} rows</span>
                </div>
                <div class="sql-table-wrap">
                  <table>
                    <thead><tr>{#each currentState.SQLResult.Columns ?? [] as column}<th>{column}</th>{/each}</tr></thead>
                    <tbody>{#each currentState.SQLResult.Rows ?? [] as row}<tr>{#each row as cell}<td>{cell}</td>{/each}</tr>{/each}</tbody>
                  </table>
                </div>
              {:else}
                <div class="empty-state"><strong>暂无 SQL 结果</strong><span>SQL 任务启动后会在这里显示查询或影响行数。</span></div>
              {/if}
            </div>
          {:else if activeTab === 'samples'}
            <div class="sample-tools">
              <label><span>数据内容检索</span><input bind:value={dataQuery} placeholder="手机号 / 邮箱 / token 值" /></label>
              <label><span>敏感匹配检索</span><input bind:value={sampleMetaQuery} placeholder="高敏感 / 密码 / token / 邮箱" /></label>
              <span>{sampleRowTotal} 条样例</span>
            </div>
            <div class="sample-scroll">
              {#each sampleGroups as group}
                <article class="sample-group">
                  <div class="sample-group-heading">
                    <div><em>数据库名</em><strong>{group.Table.Database}</strong></div>
                    <div><em>表名</em><span>{tableLabel(group.Table)}</span></div>
                    <div><em>样例行</em><span>{group.Rows.length}</span></div>
                  </div>
                  <table>
                    <thead>
                      <tr>
                        {#each group.Headers as header}
                          <th class:sample-high={sampleHeaderRisk(group.Table, header) === 'high'} class:sample-medium={sampleHeaderRisk(group.Table, header) === 'medium'} class:sample-low={sampleHeaderRisk(group.Table, header) === 'low'}>
                            <span>{header}</span>
                            {#if sampleHeaderRisk(group.Table, header)}
                              <em>{levelLabel(sampleHeaderRisk(group.Table, header))}{sampleHeaderKinds(group.Table, header) ? ` · ${sampleHeaderKinds(group.Table, header)}` : ''}</em>
                            {/if}
                          </th>
                        {/each}
                      </tr>
                    </thead>
                    <tbody>
                      {#each group.Rows as row}
                        <tr>
                          {#each group.Headers as header}
                            <td class:sample-high={sampleHeaderRisk(group.Table, header) === 'high'} class:sample-medium={sampleHeaderRisk(group.Table, header) === 'medium'} class:sample-low={sampleHeaderRisk(group.Table, header) === 'low'}>
                              {row.Values[header] ?? ''}
                            </td>
                          {/each}
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </article>
              {:else}
                <div class="empty-state"><strong>暂无样例数据</strong><span>扫描命中后会按表展示样例行。</span></div>
              {/each}
            </div>
          {:else}
            <div class="log-list">
              {#each currentState.Logs ?? [] as log}
                <code><span>{log.Time}</span> [{log.Level}] {log.Message}</code>
              {:else}
                <div class="empty-state"><strong>暂无日志</strong><span>任务运行时会滚动记录连接、扫描和输出信息。</span></div>
              {/each}
              {#each currentState.Outputs ?? [] as output}
                <button on:click={() => openOutput(output)}>打开输出目录：{output}</button>
              {/each}
            </div>
          {/if}
        </section>
      </section>
    {/if}
  </main>
{/if}
