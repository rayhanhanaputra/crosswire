// Per-page layout QA: for every tab at three widths, report anything painted
// past the right edge of its tab section, and any text clipped with no
// ellipsis and no title to recover it.
//
//   zsh qa/layout.sh
//
// Exits non-zero when any page/width combination has an issue.
const http=require('http'), WS=require('ws');
const get=u=>new Promise(r=>http.get(u,s=>{let d='';s.on('data',c=>d+=c);s.on('end',()=>r(JSON.parse(d)))}));
const TABS=['targets','repeater','intruder','history','sink'];
const WIDTHS=[1440,1100,820];
(async()=>{
  const t=(await get('http://127.0.0.1:9333/json')).find(x=>x.type==='page');
  const ws=new WS(t.webSocketDebuggerUrl); let id=0; const w={};
  ws.on('message',m=>{const o=JSON.parse(m); if(o.id&&w[o.id]){w[o.id](o);delete w[o.id];}});
  await new Promise(r=>ws.on('open',r));
  const send=(m,p)=>new Promise(r=>{const i=++id;w[i]=r;ws.send(JSON.stringify({id:i,method:m,params:p}))});
  const ev=async e=>{const r=await send('Runtime.evaluate',{expression:e,returnByValue:true,awaitPromise:true});return r.result&&r.result.result?r.result.result.value:null;};

  const probe=(tab)=>`(function(){
    const sec=document.querySelector('#tab-${tab}');
    const right=sec.getBoundingClientRect().right;
    const spills=[], clipped=[];
    for (const el of sec.querySelectorAll('*')) {
      if(!el.offsetWidth&&!el.offsetHeight) continue;
      const cs=getComputedStyle(el);
      const inScroll=(()=>{let p=el.parentElement;while(p&&p!==document.body){const c=getComputedStyle(p);
        if(c.overflowX==='auto'||c.overflowX==='scroll')return true;p=p.parentElement;}return false;})();
      const name=el.tagName.toLowerCase()+(el.className?'.'+String(el.className).trim().split(/\\s+/).join('.'):'');
      const s=Math.round(el.getBoundingClientRect().right-right);
      if(s>2 && !inScroll) spills.push({s,name,txt:(el.textContent||'').trim().replace(/\\s+/g,' ').slice(0,36)});
      // text cut off with no ellipsis and no title to recover it
      if(!el.children.length && el.scrollWidth-el.clientWidth>2 && cs.overflowX==='hidden'
         && cs.textOverflow!=='ellipsis' && !el.title)
        clipped.push({name, txt:(el.textContent||'').trim().slice(0,36)});
    }
    const u=(arr,k)=>{const s=new Set(),o=[];for(const x of arr.sort((a,b)=>(b.s||0)-(a.s||0))){if(s.has(x[k]))continue;s.add(x[k]);o.push(x);}return o;};
    return JSON.stringify({page: document.documentElement.scrollWidth-document.documentElement.clientWidth,
                           spills:u(spills,'name').slice(0,5), clipped:u(clipped,'name').slice(0,4)});
  })()`;

  let bad=0;
  for (const width of WIDTHS) {
    await send('Emulation.setDeviceMetricsOverride',{width,height:900,deviceScaleFactor:1,mobile:false});
    for (const tab of TABS) {
      await ev(`location.hash='#${tab}'; true`);
      await new Promise(r=>setTimeout(r,250));
      if (tab==='history') { await ev("(function(){const r=document.querySelector('#tab-history tr.click'); r&&r.click(); return true})()"); await new Promise(r=>setTimeout(r,900)); }
      const res=JSON.parse(await ev(probe(tab)));
      // A split whose tall side is the narrow one wastes the page: the run
      // list sat in 909px of nothing while the detail crammed into 479.
      const prop=JSON.parse(await ev(`(function(){
        const s=document.querySelector('#tab-${tab} .split');
        if(!s||getComputedStyle(s).gridTemplateColumns.split(' ').length<2) return 'null';
        const kids=[...s.children];
        const [a,b]=kids.map(c=>c.getBoundingClientRect());
        // A side showing only its empty state is short because nothing has run
        // yet, not because it was starved of width.
        const idle=c=>{const p=c.querySelectorAll('.pane'); return p.length>0 && [...p].every(x=>x.classList.contains('idle'));};
        const starved = (!idle(kids[1]) && a.height>b.height*2 && a.width<b.width*0.6) ||
                        (!idle(kids[0]) && b.height>a.height*2 && b.width<a.width*0.6);
        return JSON.stringify(starved? {aw:Math.round(a.width),ah:Math.round(a.height),bw:Math.round(b.width),bh:Math.round(b.height)} : null);
      })()`));
      const ok = !res.spills.length && !res.clipped.length && res.page<=1 && !prop;
      if(!ok) bad++;
      console.log(`${String(width).padEnd(5)} ${tab.padEnd(10)} ${ok?'ok':'ISSUES'}${res.page>1?'  pageScroll='+res.page:''}`);
      for(const s of res.spills)  console.log(`        spill +${String(s.s).padStart(4)}px ${s.name.slice(0,42).padEnd(44)} ${JSON.stringify(s.txt)}`);
      for(const c of res.clipped) console.log(`        clipped          ${c.name.slice(0,42).padEnd(44)} ${JSON.stringify(c.txt)}`);
      if(prop) console.log(`        starved column: the taller side is the narrower one (${prop.aw}x${prop.ah} vs ${prop.bw}x${prop.bh})`);
    }
  }
  console.log(bad? `\n${bad} page/width combinations have issues` : '\nall pages clean at all widths');
  ws.close(); process.exit(bad ? 1 : 0);
})();
