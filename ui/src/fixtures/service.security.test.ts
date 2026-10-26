import {expect,it} from "vitest";
import {fakeService} from "./service";
it("models one-use action confirmation in stories",async()=>{
 const service=fakeService();const request={action:"kill",session_id:1,machine:"local"};
 const call=(path:string,body:unknown)=>service(path,{method:"POST",body:JSON.stringify(body)});
 expect((await call("/app/action",{...request,confirmed:true})).status).toBe(403);
 const {token}=await(await call("/app/confirm",{message:"Stop?",request})).json();
 expect((await call("/app/action",{...request,token})).status).toBe(200);
 expect((await call("/app/action",{...request,token})).status).toBe(403);
});
