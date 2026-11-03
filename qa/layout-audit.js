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
    // ---- misalignment: things that are not clipped or spilling, just crooked.
    // Overflow checks never saw the Targets header, whose right-hand control
    // pair wrapped onto a second line and left the heading 28px off centre.
    const crooked=[], ragged=[], heads=[];
    const vis=el=>el.offsetWidth||el.offsetHeight;
    const mid=r=>r.top+r.height/2;

    // 1. A .row lays its children on one line. If their vertical centres
    //    disagree the row has wrapped, and nothing in it is level any more.
    for (const row of sec.querySelectorAll('.row')) {
      if(!vis(row)) continue;
      const kids=[...row.children].filter(vis);
      if(kids.length<2) continue;
      const cs=kids.map(k=>mid(k.getBoundingClientRect()));
      const spread=Math.max(...cs)-Math.min(...cs);
      if(spread>3) crooked.push({spread:Math.round(spread),name:'.'+String(row.className).trim().split(/\\s+/).join('.'),
                                 txt:(row.textContent||'').trim().replace(/\\s+/g,' ').slice(0,34)});
    }

    // 2. Controls sharing a row of a field grid share a centre line. A
    //    checkbox has no label above it, so it floats up unless it is padded.
    for (const grid of sec.querySelectorAll('.grid2, .run-opts')) {
      if(!vis(grid)) continue;
      const rows=[];
      for (const cell of [...grid.children].filter(vis)) {
        const t=cell.getBoundingClientRect().top;
        let r=rows.find(r=>Math.abs(r.t-t)<8); if(!r){r={t,cells:[]};rows.push(r);} r.cells.push(cell);
      }
      for (const r of rows) {
        const got=r.cells.map(c=>{const e=c.querySelector('input,select,textarea'); if(!e||!vis(e))return null;
          const q=e.getBoundingClientRect();
          // a textarea is taller on purpose: compare the band it starts in
          return e.tagName==='TEXTAREA' ? q.top+17 : mid(q);}).filter(x=>x!=null);
        if(got.length<2) continue;
        const spread=Math.max(...got)-Math.min(...got);
        if(spread>2) ragged.push({spread:Math.round(spread),name:'.'+String(grid.className).trim().split(/\\s+/).join('.'),
                                  txt:(r.cells.map(c=>(c.textContent||'').trim().slice(0,14)).join(' / ')).slice(0,42)});
      }
    }

    // 3. The two sides of a split open at the same height, so their heading
    //    strips are one line across the page, not a step.
    for (const split of sec.querySelectorAll('.split')) {
      if(!vis(split)) continue;
      if(getComputedStyle(split).gridTemplateColumns.split(' ').length<2) continue;
      const tops=[...split.children].filter(vis).map(col=>{
        const pane=col.classList.contains('pane')?col:col.querySelector('.pane');
        if(!pane) return null;
        // Every pane opens on a ruled strip, empty ones included, so every
        // strip on the page is one line across or it is a step.
        const hd=pane.querySelector('h2');
        return hd&&vis(hd)?{top:Math.round(hd.getBoundingClientRect().top),txt:hd.textContent.trim().slice(0,22)}:null;
      }).filter(Boolean);
      if(tops.length<2) continue;
      const spread=Math.max(...tops.map(x=>x.top))-Math.min(...tops.map(x=>x.top));
      if(spread>1) heads.push({spread,txt:tops.map(x=>x.txt+'@'+x.top).join(' vs ')});
    }

    // 4. The shell is the original's: edge-to-edge columns divided by one
    //    hairline, panes with no frame of their own. None of the checks above
    //    can see a pane that floats again — a rounded card on the grey field
    //    is perfectly aligned and perfectly clipped-proof. Measure the model.
    const floats=[];
    for (const pane of sec.querySelectorAll('.pane')) {
      if(!vis(pane)) continue;
      const cs=getComputedStyle(pane);
      const name='.'+String(pane.className).trim().split(/\s+/g).join('.');
      if(cs.borderRadius!=='0px') floats.push({name,why:'radius '+cs.borderRadius});
      if(cs.boxShadow!=='none')   floats.push({name,why:'shadow'});
      // a frame is allowed only where a divider belongs: top rule on a stacked
      // pane, left rule on a second column. A border on any other side is a card.
      const sides=[['top',cs.borderTopWidth],['right',cs.borderRightWidth],['bottom',cs.borderBottomWidth],['left',cs.borderLeftWidth]].filter(([,w])=>w!=='0px');
      if(sides.some(([side])=>!['top','left'].includes(side)))
        floats.push({name,why:'border '+sides.map(x=>x[0]).join('+')});
    }
    // columns touch: no gutter of grey field between two panes of one split.
    for (const split of sec.querySelectorAll('.split')) {
      if(!vis(split)) continue;
      if(getComputedStyle(split).columnGap!=='0px')
        floats.push({name:'.split',why:'column-gap '+getComputedStyle(split).columnGap});
      for (const col of [...split.children].filter(vis)) {
        const panes=[...col.querySelectorAll(':scope > .pane')].filter(vis);
        for(let i=1;i<panes.length;i++){
          const gap=Math.round(panes[i].getBoundingClientRect().top-panes[i-1].getBoundingClientRect().bottom);
          if(gap!==0) floats.push({name:'.split pane stack',why:gap+'px gap between stacked panes'});
        }
      }
    }

    const u=(arr,k)=>{const s=new Set(),o=[];for(const x of arr.sort((a,b)=>(b.s||b.spread||0)-(a.s||a.spread||0))){if(s.has(x[k]))continue;s.add(x[k]);o.push(x);}return o;};
    return JSON.stringify({page: document.documentElement.scrollWidth-document.documentElement.clientWidth,
                           spills:u(spills,'name').slice(0,5), clipped:u(clipped,'name').slice(0,4),
                           crooked:u(crooked,'name').slice(0,4), ragged:u(ragged,'txt').slice(0,4), heads:heads.slice(0,2),
                           floats:u(floats,'name').slice(0,4)});
  })()`;

  let bad=0;
  for (const width of WIDTHS) {
    await send('Emulation.setDeviceMetricsOverride',{width,height:900,deviceScaleFactor:1,mobile:false});
    for (const tab of TABS) {
      await ev(`location.hash='#${tab}'; true`);
      await new Promise(r=>setTimeout(r,250));
      if (tab==='history') {
        await ev("(function(){const r=document.querySelector('#tab-history tr.click'); r&&r.click(); return true})()");
        await new Promise(r=>setTimeout(r,900));
        // Expand a trial so the detail grid is audited too, not just the table.
        await ev("(function(){const b=[...document.querySelectorAll('#tab-history button.link')].find(x=>x.textContent==='details'); b&&b.click(); return true})()");
        await new Promise(r=>setTimeout(r,300));
      }
      // The Targets form hides most of its fields until a transport is picked
      // and the two disclosures are opened, so audit it with everything shown.
      if (tab==='targets') {
        await ev("(function(){const o=document.querySelector('#sec-observer'),m=document.querySelector('#sec-mappers'); if(o)o.open=true; if(m)m.open=true; return true})()");
        await new Promise(r=>setTimeout(r,250));
      }
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
      const ok = !res.spills.length && !res.clipped.length && res.page<=1 && !prop
                 && !res.crooked.length && !res.ragged.length && !res.heads.length
                 && !res.floats.length;
      if(!ok) bad++;
      console.log(`${String(width).padEnd(5)} ${tab.padEnd(10)} ${ok?'ok':'ISSUES'}${res.page>1?'  pageScroll='+res.page:''}`);
      for(const s of res.spills)  console.log(`        spill +${String(s.s).padStart(4)}px ${s.name.slice(0,42).padEnd(44)} ${JSON.stringify(s.txt)}`);
      for(const c of res.clipped) console.log(`        clipped          ${c.name.slice(0,42).padEnd(44)} ${JSON.stringify(c.txt)}`);
      if(prop) console.log(`        starved column: the taller side is the narrower one (${prop.aw}x${prop.ah} vs ${prop.bw}x${prop.bh})`);
      for(const c of res.crooked) console.log(`        wrapped row: children sit ${c.spread}px apart, so nothing in it is level  ${c.name.slice(0,30).padEnd(32)} ${JSON.stringify(c.txt)}`);
      for(const r of res.ragged)  console.log(`        ragged field row: controls off one centre line by ${r.spread}px  ${r.name.slice(0,22).padEnd(24)} ${JSON.stringify(r.txt)}`);
      for(const hd of res.heads)  console.log(`        split headings step by ${hd.spread}px instead of opening level: ${hd.txt}`);
      for(const f of res.floats)  console.log(`        floating card: ${f.why.padEnd(30)} ${f.name}`);
    }
  }
  console.log(bad? `\n${bad} page/width combinations have issues` : '\nall pages clean at all widths');
  ws.close(); process.exit(bad ? 1 : 0);
})();
