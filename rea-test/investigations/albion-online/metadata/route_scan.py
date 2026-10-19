"""Bounded scan of methods taking Photon g7s + short, retaining comparisons.
Only immediates in decoded CMP/SUB are candidate code tests, not proven routing.
"""
import json
from disassemble import *
methods=json.loads((OUT/'focus-candidates.json').read_text(encoding='utf-8'))['methods']
cs=[]
for me in methods:
    if me['image']!='Albion.PhotonClient.dll' or not me['native_va']:continue
    pars=me['parameters']
    if len(pars)<2 or not any(p['type']=='g7s' for p in pars) or not any(p['type']=='short' for p in pars):continue
    try:
        obj=disassemble(int(me['native_va'],16),True)
    except ValueError:
        continue
    compares=[r for r in obj['instructions'] if r['mnemonic'] in ['cmp','sub'] and ', 0x1d' in r['operands']]
    if compares:
        cs.append(dict(method=me,comparisons=compares,disassembly_evidence_id=obj['evidence_id']))
        print(me['index'],me['type'],me['name'],me['native_va'],len(obj['instructions']),compares)
emit('new-character-route-candidates.json',dict(evidence_id='AO-META-ROUTE-SCAN-001',methods_scanned=sum(1 for me in methods if me['image']=='Albion.PhotonClient.dll' and me['native_va'] and len(me['parameters'])>=2 and any(p['type']=='g7s' for p in me['parameters']) and any(p['type']=='short' for p in me['parameters'])),candidates=cs,limitations='CMP/SUB immediate29 search in g7s/short methods only. Candidate comparisons do not establish event routing.'))
