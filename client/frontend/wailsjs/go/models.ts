export namespace main {
	
	export class BindRequest {
	    server_url: string;
	    bind_code: string;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new BindRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server_url = source["server_url"];
	        this.bind_code = source["bind_code"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}

}

