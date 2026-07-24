import puppeteer from "puppeteer-core";
const B="http://localhost:5173";
const b=await puppeteer.launch({executablePath:"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",headless:"new",args:["--no-sandbox"]});
const S=ms=>new Promise(r=>setTimeout(r,ms));
const p=await b.newPage(); await p.setViewport({width:1400,height:900,deviceScaleFactor:1.5});
const errs=[]; p.on("pageerror",e=>errs.push("PAGEERROR "+String(e).slice(0,140)));
await p.goto(B+"/mail",{waitUntil:"networkidle2"});
if(await p.$('input[type="password"]')){ await p.type('input[type="email"]',"ana@acme-dev.com"); await p.type('input[type="password"]',"correct-horse-battery-9273"); await p.click("form.login button.send"); await S(3000); }
await p.goto(B+"/mail/mail",{waitUntil:"networkidle2"}); await S(2200);
const antes=await p.evaluate(()=>({n:document.querySelectorAll(".row").length, primeiro:document.querySelector(".row .s")?.innerText}));
console.log("antes:", JSON.stringify(antes));
// arrastar a 1ª mensagem para a pasta "Teste"
const src=await p.evaluate(()=>{const r=document.querySelector(".row").getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2};});
const dst=await p.evaluate(()=>{const b=[...document.querySelectorAll(".mrail .nav button")].find(x=>x.innerText.includes("Teste")); if(!b) return null; const r=b.getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2};});
if(!dst){ console.log("pasta Teste não existe"); await b.close(); process.exit(0); }
await p.mouse.move(src.x,src.y); await p.mouse.down();
await p.mouse.move(src.x-100,src.y+20,{steps:8}); await p.mouse.move(dst.x,dst.y,{steps:12}); await S(500);
console.log("pasta acende ao passar por cima:", await p.evaluate(()=>!!document.querySelector(".mrail .nav button.drop")));
await p.mouse.up(); await S(2000);
const dep=await p.evaluate(()=>({n:document.querySelectorAll(".row").length, primeiro:document.querySelector(".row .s")?.innerText,
  toast:document.querySelector(".toast, [class*=toast]")?.innerText}));
console.log("depois:", JSON.stringify(dep));
console.log("erros:", errs.length?errs.join("\n"):"nenhum");
await b.close();
