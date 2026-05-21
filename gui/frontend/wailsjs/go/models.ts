export namespace app {
	
	export class ConnectionTestResult {
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
	
	    static createFrom(source: any = {}) {
	        return new ConnectionTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Success = source["Success"];
	        this.Message = source["Message"];
	        this.Type = source["Type"];
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.Database = source["Database"];
	        this.User = source["User"];
	        this.Proxy = source["Proxy"];
	        this.Version = source["Version"];
	        this.ResolvedAddr = source["ResolvedAddr"];
	        this.ServerTime = source["ServerTime"];
	    }
	}
	export class CustomSQLResult {
	    Columns: string[];
	    Rows: string[][];
	    Total: number;
	    Shown: number;
	    Affected: number;
	    IsQuery: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CustomSQLResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Columns = source["Columns"];
	        this.Rows = source["Rows"];
	        this.Total = source["Total"];
	        this.Shown = source["Shown"];
	        this.Affected = source["Affected"];
	        this.IsQuery = source["IsQuery"];
	    }
	}
	export class FscanTargetPreview {
	    Type: string;
	    Host: string;
	    Port: number;
	    User: string;
	    Line: number;
	    Raw: string;
	    Password: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FscanTargetPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Type = source["Type"];
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.User = source["User"];
	        this.Line = source["Line"];
	        this.Raw = source["Raw"];
	        this.Password = source["Password"];
	    }
	}
	export class FscanPreview {
	    Targets: FscanTargetPreview[];
	    Total: number;
	
	    static createFrom(source: any = {}) {
	        return new FscanPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Targets = this.convertValues(source["Targets"], FscanTargetPreview);
	        this.Total = source["Total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class LogEntry {
	    Time: string;
	    Level: string;
	    Message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Time = source["Time"];
	        this.Level = source["Level"];
	        this.Message = source["Message"];
	    }
	}
	export class ScanRequest {
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
	
	    static createFrom(source: any = {}) {
	        return new ScanRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Type = source["Type"];
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.User = source["User"];
	        this.Password = source["Password"];
	        this.Database = source["Database"];
	        this.Table = source["Table"];
	        this.Proxy = source["Proxy"];
	        this.Mode = source["Mode"];
	        this.Level = source["Level"];
	        this.Limit = source["Limit"];
	        this.SQL = source["SQL"];
	        this.Output = source["Output"];
	        this.Fscan = source["Fscan"];
	        this.FscanText = source["FscanText"];
	        this.SplitOutput = source["SplitOutput"];
	        this.IncludeSystem = source["IncludeSystem"];
	        this.Mask = source["Mask"];
	        this.Workers = source["Workers"];
	        this.Timeout = source["Timeout"];
	    }
	}
	export class ScanJobState {
	    JobID: string;
	    Status: string;
	    Progress: number;
	    Message: string;
	    TargetLabel: string;
	    Request: ScanRequest;
	    ServerInfo?: db.ServerInfo;
	    RedisInfo?: redis.Info;
	    Result: scanner.Result;
	    SQLResult?: CustomSQLResult;
	    Outputs: string[];
	    Logs: LogEntry[];
	    Errors: string[];
	    StartedAt: string;
	    FinishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ScanJobState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.JobID = source["JobID"];
	        this.Status = source["Status"];
	        this.Progress = source["Progress"];
	        this.Message = source["Message"];
	        this.TargetLabel = source["TargetLabel"];
	        this.Request = this.convertValues(source["Request"], ScanRequest);
	        this.ServerInfo = this.convertValues(source["ServerInfo"], db.ServerInfo);
	        this.RedisInfo = this.convertValues(source["RedisInfo"], redis.Info);
	        this.Result = this.convertValues(source["Result"], scanner.Result);
	        this.SQLResult = this.convertValues(source["SQLResult"], CustomSQLResult);
	        this.Outputs = source["Outputs"];
	        this.Logs = this.convertValues(source["Logs"], LogEntry);
	        this.Errors = source["Errors"];
	        this.StartedAt = source["StartedAt"];
	        this.FinishedAt = source["FinishedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace db {
	
	export class ServerInfo {
	    Host: string;
	    Port: number;
	    DBType: string;
	    Version: string;
	    CurrentUser: string;
	    CurrentDB: string;
	    ServerTime: string;
	    Environment: Record<string, string>;
	    ResolvedAddr: string;
	    Proxy: string;
	    IncludeSystem: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.DBType = source["DBType"];
	        this.Version = source["Version"];
	        this.CurrentUser = source["CurrentUser"];
	        this.CurrentDB = source["CurrentDB"];
	        this.ServerTime = source["ServerTime"];
	        this.Environment = source["Environment"];
	        this.ResolvedAddr = source["ResolvedAddr"];
	        this.Proxy = source["Proxy"];
	        this.IncludeSystem = source["IncludeSystem"];
	    }
	}

}

export namespace redis {
	
	export class Info {
	    Host: string;
	    Port: number;
	    Version: string;
	    Mode: string;
	    DB: string;
	    Keyspace: string;
	    ResolvedIP: string;
	    Proxy: string;
	    ServerTime: string;
	    RequireAuth: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.Version = source["Version"];
	        this.Mode = source["Mode"];
	        this.DB = source["DB"];
	        this.Keyspace = source["Keyspace"];
	        this.ResolvedIP = source["ResolvedIP"];
	        this.Proxy = source["Proxy"];
	        this.ServerTime = source["ServerTime"];
	        this.RequireAuth = source["RequireAuth"];
	    }
	}

}

export namespace scanner {
	
	export class FieldResult {
	    Name: string;
	    Kinds: string[];
	    Level: string;
	    Mode: string;
	    Total: number;
	
	    static createFrom(source: any = {}) {
	        return new FieldResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Kinds = source["Kinds"];
	        this.Level = source["Level"];
	        this.Mode = source["Mode"];
	        this.Total = source["Total"];
	    }
	}
	export class RowSample {
	    Values: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new RowSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Values = source["Values"];
	    }
	}
	export class TableResult {
	    Database: string;
	    Schema: string;
	    Name: string;
	    Total: number;
	    Columns: string[];
	    Fields: FieldResult[];
	    Rows: RowSample[];
	
	    static createFrom(source: any = {}) {
	        return new TableResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Database = source["Database"];
	        this.Schema = source["Schema"];
	        this.Name = source["Name"];
	        this.Total = source["Total"];
	        this.Columns = source["Columns"];
	        this.Fields = this.convertValues(source["Fields"], FieldResult);
	        this.Rows = this.convertValues(source["Rows"], RowSample);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Sample {
	    Database: string;
	    Schema: string;
	    Table: string;
	    Column: string;
	    Kind: string;
	    Level: string;
	    Mode: string;
	    Value: string;
	
	    static createFrom(source: any = {}) {
	        return new Sample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Database = source["Database"];
	        this.Schema = source["Schema"];
	        this.Table = source["Table"];
	        this.Column = source["Column"];
	        this.Kind = source["Kind"];
	        this.Level = source["Level"];
	        this.Mode = source["Mode"];
	        this.Value = source["Value"];
	    }
	}
	export class Summary {
	    Database: string;
	    Schema: string;
	    Table: string;
	    Column: string;
	    Kind: string;
	    Level: string;
	    Mode: string;
	    Total: number;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Database = source["Database"];
	        this.Schema = source["Schema"];
	        this.Table = source["Table"];
	        this.Column = source["Column"];
	        this.Kind = source["Kind"];
	        this.Level = source["Level"];
	        this.Mode = source["Mode"];
	        this.Total = source["Total"];
	    }
	}
	export class Result {
	    Summaries: Summary[];
	    Samples: Sample[];
	    Tables: TableResult[];
	    Errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Summaries = this.convertValues(source["Summaries"], Summary);
	        this.Samples = this.convertValues(source["Samples"], Sample);
	        this.Tables = this.convertValues(source["Tables"], TableResult);
	        this.Errors = source["Errors"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	

}

