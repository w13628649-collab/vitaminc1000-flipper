/* REA ev_1432a2360489cab337f425cde48c2fe3f515b2e1eb2919145f4d50970f6e5657; source response evidence/new-character-message-handler.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180aacaf0(undefined8 param_1,undefined8 param_2,undefined2 param_3)

{
  undefined4 uVar1;
  undefined8 uVar2;
  undefined8 uVar3;
  undefined8 uVar4;
  code *pcVar5;
  undefined8 uVar6;
  undefined8 uVar7;
  longlong lVar8;
  longlong *plVar9;
  longlong *plVar10;
  longlong lVar11;
  undefined8 local_48;
  undefined8 uStack_40;
  
  if (DAT_18507b491 == '\0') {
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184dd6228);
    FUN_180361910(&DAT_184e0ac30);
    FUN_180361910(&DAT_184e0be10);
    FUN_180361910(&DAT_184e1c160);
    FUN_180361910(&DAT_184e1e768);
    DAT_18507b491 = '\x01';
  }
  if (**(char **)(_DAT_184e0be10 + 0xb8) != '\0') {
    return;
  }
  lVar8 = FUN_182003eb0(param_2,_DAT_184dd6228);
  if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
    il2cpp_runtime_class_init();
  }
  if (DAT_18507aae8 == '\0') {
    FUN_180361910(&DAT_184e0ac30);
    DAT_18507aae8 = '\x01';
  }
  if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
    il2cpp_runtime_class_init();
  }
  plVar9 = (longlong *)FUN_180361710(_DAT_184e0ac30);
  if ((lVar8 != 0) && (*plVar9 != 0)) {
    plVar9 = (longlong *)FUN_18096f090(*plVar9,*(undefined8 *)(lVar8 + 0x10),0);
    if (plVar9 != (longlong *)0x0) {
      lVar11 = *plVar9;
      if ((*(byte *)(_DAT_184e1e768 + 0x130) <= *(byte *)(lVar11 + 0x130)) &&
         (*(longlong *)
           (*(longlong *)(lVar11 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1e768 + 0x130) * 8) ==
          _DAT_184e1e768)) {
        (**(code **)(lVar11 + 0x238))(plVar9,param_2,param_3,*(undefined8 *)(lVar11 + 0x240));
        return;
      }
    }
    lVar11 = *(longlong *)(lVar8 + 0x18);
    uVar6 = *(undefined8 *)(lVar8 + 0x40);
    uVar7 = *(undefined8 *)(lVar8 + 0x48);
    if (lVar11 == 0) {
      lVar11 = **(longlong **)(_DAT_185095fd0 + 0xb8);
    }
    uVar1 = *(undefined4 *)(lVar8 + 0x28);
    uVar2 = *(undefined8 *)(lVar8 + 0x10);
    uVar3 = *(undefined8 *)(lVar8 + 0x1a0);
    uVar4 = *(undefined8 *)(lVar8 + 0x1a8);
    plVar9 = (longlong *)thunk_FUN_18039ec10(_DAT_184e1c160);
    local_48 = uVar6;
    uStack_40 = uVar7;
    FUN_180bd6ea0(plVar9,uVar2,uVar1,&local_48,lVar11,uVar3,uVar4,0);
    if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    if (DAT_18507aae8 == '\0') {
      FUN_180361910(&DAT_184e0ac30);
      DAT_18507aae8 = '\x01';
    }
    if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar10 = (longlong *)FUN_180361710(_DAT_184e0ac30);
    if ((*plVar10 != 0) && (FUN_18096ee00(*plVar10,plVar9,0), plVar9 != (longlong *)0x0)) {
      (**(code **)(*plVar9 + 0x238))(plVar9,param_2,param_3,*(undefined8 *)(*plVar9 + 0x240));
      if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
        il2cpp_runtime_class_init();
      }
      if (DAT_18507a9a2 == '\0') {
        FUN_180361910(&DAT_184e08f58);
        DAT_18507a9a2 = '\x01';
      }
      if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
        il2cpp_runtime_class_init();
      }
      plVar9 = (longlong *)FUN_180361710(_DAT_184e08f58);
      if ((*plVar9 != 0) && (lVar11 = *(longlong *)(*plVar9 + 0x250), lVar11 != 0)) {
        FUN_180a5e900(lVar11,lVar8,0);
        return;
      }
    }
  }
  FUN_180361b60();
  pcVar5 = (code *)swi(3);
  (*pcVar5)();
  return;
}


