import fs from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { createHash } from "node:crypto";
const reference=resolve(process.env.YUKINO_REFERENCE ?? resolve(homedir(),"github/yukino-code/apps/yukino"));
const {buildOpenAIInput}=await import(pathToFileURL(resolve(reference,"src/llm/openai.ts")).href);
const image={type:"image",source:{type:"base64",media_type:"image/png",data:"aGVsbG8="}};
const actions=[{type:"click",x:10,y:20},{type:"double_click",x:1,y:2},{type:"drag",path:[{x:1,y:2},{x:2,y:3}],keys:["CTRL"]},{type:"keypress",keys:["ENTER"]},{type:"move",x:1,y:2},{type:"scroll",x:0,y:0,scrollX:100,scrollY:-200},{type:"screenshot"},{type:"type",text:"中文"},{type:"wait"},{type:"unknown"},{type:"click",x:0}];
const cases=[
 [{role:"assistant",content:"",toolUses:[{toolUseId:"call-1",toolName:"ComputerUse",providerItemId:"cu_1",arguments:{actions,pendingSafetyChecks:[{id:"safety-1",code:"",message:"check"}],status:"in_progress"}}]},{role:"user",content:"",toolResults:[{toolUseId:"call-1",content:"screenshot",contentBlocks:[image],isError:false}]}],
 [{role:"assistant",content:"",toolUses:[{toolUseId:"call-1",toolName:"ComputerUse",arguments:{actions:[],status:"unknown"}}]},{role:"user",content:"",toolResults:[{toolUseId:"call-1",content:"failed",isError:true}]}],
 [{role:"assistant",content:"calling",toolUses:[{toolUseId:"tool-1",toolName:"Fetch",arguments:{url:"https://example.com"}}]},{role:"user",content:"trailing",toolResults:[{toolUseId:"tool-1",content:"output",contentBlocks:[image,{type:"document",title:"report",source:{type:"text",data:"文档"}},{type:"document",source:{type:"base64",data:"cGRm"}},{type:"document",source:{type:"url",url:"https://example.com/report.pdf"}}]}]}],
 [{role:"user",content:"",toolResults:[{toolUseId:"orphan",content:"plain",contentBlocks:[{type:"text",text:"ignored"}]}]}],
 [{role:"assistant",content:"answer",thinkingBlocks:[{type:"thinking",thinking:"reason",signature:"rs_test"},{type:"thinking",thinking:"omitted",signature:"other"}]}],
];
fs.writeFileSync(new URL("openai-reference.json",import.meta.url),JSON.stringify({sha256:createHash("sha256").update(fs.readFileSync(resolve(reference,"src/llm/openai.ts"))).digest("hex"),cases:cases.map(messages=>({messages,expected:buildOpenAIInput(messages)}))},null,2)+"\n");
