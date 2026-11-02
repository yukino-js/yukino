import {homedir} from "node:os";
import {resolve} from "node:path";
import {pathToFileURL,fileURLToPath} from "node:url";
import {createHash} from "node:crypto";
import {execFileSync} from "node:child_process";
const reference=resolve(process.env.YUKINO_REFERENCE ?? resolve(homedir(),"github/yukino-code/apps/yukino"));
const fromReference=(path:string)=>import(pathToFileURL(resolve(reference,path)).href);
import fs from "node:fs";
const {default:React}=await fromReference("node_modules/react/index.js");
const {renderToString,render}=await fromReference("node_modules/ink/build/index.js");
const {default:StdoutContext}=await fromReference("node_modules/ink/build/components/StdoutContext.js");
const {AvailableRows}=await fromReference("src/ui/use-terminal-layout.ts");
const {ToolCard}=await fromReference("src/ui/tool-display.tsx");
const {ThinkingBlock}=await fromReference("src/ui/thinking-block.tsx");
const {ThinkingSelect}=await fromReference("src/ui/thinking-select.tsx");
const {PermissionDialog}=await fromReference("src/ui/permission-dialog.tsx");
const {AskUserDialog}=await fromReference("src/ui/ask-user-dialog.tsx");
const {AgentsDialog}=await fromReference("src/ui/agents-dialog.tsx");
const {WorkflowsDialog}=await fromReference("src/ui/workflows-dialog.tsx");
function normalize(text:string){return text.replace(/\x1b\[[0-9;]*m/g,"").split("\n").map(x=>x.trimEnd()).join("\n").replace(/\n+$/,"");}
function draw(component:any,props:any,width:number,rows:number){return normalize(renderToString(React.createElement(StdoutContext.Provider,{value:{stdout:{columns:width,rows:24,isTTY:false,on(){},off(){}},write(){}}},React.createElement(AvailableRows.Provider,{value:rows},React.createElement(component,props))),{columns:width}));}
const noop=()=>{};
const emptyRuns:any[]=[];
const cards=[];
for(const width of [1,2,5,12,24,40,80]){
 for(const props of [
 {toolName:"ReadFile",argsSummary:"a.go",output:"source body\nnext line",elapsed:1.2},
 {toolName:"Bash",argsSummary:"echo hello",output:"hello",status:"running",elapsed:1.1,progress:"(Ctrl+B to run in background)"},
 {toolName:"PowerShell",argsSummary:"Write-Host one\nWrite-Host two",output:"failure",status:"failed"},
 {toolName:"EditFile",argsSummary:"a.go",output:"+ added\n- removed\ncontext",status:"stopped"},
 {toolName:"Agent",argsSummary:"explore",output:"result",status:"stopped",progress:"explore subagent | 3 turns"},
 ]) cards.push({width,props,expected:draw(ToolCard,props,width,20)});
}
const thinking=[];
for(const width of [1,2,5,12,24,80])for(const expanded of [false,true])for(const streaming of [false,true]){const props={text:"first reasoning\n\nsecond reasoning",duration:streaming?undefined:1.5,expanded,streaming};thinking.push({width,props,expected:draw(ThinkingBlock,props,width,20)});}
const {VirtualTerminal}=await fromReference("tests/helpers/virtual-terminal.ts");
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
process.setMaxListeners(0);
async function drawLive(component:any,props:any,width:number,rows:number){
 const terminal=new VirtualTerminal(width,24);
 let instance:any;
 await React.act(async()=>{instance=render(React.createElement(AvailableRows.Provider,{value:rows},React.createElement(component,props)),{stdout:terminal.stdout,stdin:terminal.stdin,patchConsole:false,exitOnCtrlC:false,interactive:true,maxFps:1000});});
 await instance.waitUntilRenderFlush();
 await terminal.flush();
 const output=terminal.bufferLines().join("\n");
 await React.act(async()=>{instance.unmount();await instance.waitUntilExit();instance.cleanup();});
 terminal.dispose();
 return normalize(output);
}
const dialogs=[];
const qs=[{question:"Choose a color",options:[{label:"Blue",description:"Sky color"},{label:"Green",description:"Grass color"}],multiSelect:false}];
for(const width of [1,2,5,12,24,40,80])for(let rows=1;rows<=24;rows++){
 for(const [kind,component,props] of [
 ["thinking",ThinkingSelect,{currentLevel:"high",levels:["off","minimal","low","medium","high","xhigh","max"],onSelect:noop,onCancel:noop}],
 ["permission",PermissionDialog,{toolName:"Bash",argsSummary:"go test",reason:"Approval required",cwd:"/project",rememberPatterns:["Bash(go test*)"],onComplete:noop}],
 ["ask",AskUserDialog,{questions:qs,onComplete:noop}],
 ["agents",AgentsDialog,{teammates:[],subagents:[],backgroundTasks:[{id:"agent-1",name:"Inspect files",status:"running",kind:"agent",output:"partial output\nnext line"}],onClose:noop,onStopBackground:noop}],
 ["workflows",WorkflowsDialog,{service:{store:{subscribe:()=>noop,getSnapshot:()=>emptyRuns},loadPersistedRuns:async()=>{}},context:()=>({}),onClose:noop}],
 ] as any[])dialogs.push({kind,width,rows,expected:await drawLive(component,props,width,rows)});
}
fs.writeFileSync(fileURLToPath(new URL("m18-components.json",import.meta.url)),JSON.stringify({reference:referenceMetadata(),cards,thinking,dialogs},null,2)+"\n");

function referenceMetadata(){
 return {commit:execFileSync("git",["-C",reference,"rev-parse","HEAD"],{encoding:"utf8"}).trim(),files:Object.fromEntries(["src/ui/use-terminal-layout.ts", "src/ui/tool-display.tsx", "src/ui/thinking-block.tsx", "src/ui/thinking-select.tsx", "src/ui/permission-dialog.tsx", "src/ui/ask-user-dialog.tsx", "src/ui/agents-dialog.tsx", "src/ui/workflows-dialog.tsx"].map(path=>[path,createHash("sha256").update(fs.readFileSync(resolve(reference,path))).digest("hex")]))};
}
