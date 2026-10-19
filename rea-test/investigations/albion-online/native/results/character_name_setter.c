/* REA ev_e6913f7f59d91e7c49745ea72241e03b2f13b76e860498d2bf120b3fd623a34a; source response evidence/character_name_setter.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180bd9010(longlong param_1,longlong param_2)

{
  code *pcVar1;
  char cVar2;
  longlong *plVar3;
  undefined8 uVar4;
  longlong lVar5;
  
  if (DAT_18507bf99 == '\0') {
    FUN_180361910(&DAT_184dfbeb8);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184e0ac30);
    FUN_180361910(&DAT_184e4aab8);
    DAT_18507bf99 = '\x01';
  }
  if (param_2 == 0) {
    param_2 = **(longlong **)(_DAT_185095fd0 + 0xb8);
  }
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
  plVar3 = (longlong *)FUN_180361710(_DAT_184e0ac30);
  if (*plVar3 != 0) {
    cVar2 = FUN_180973590(*plVar3,0);
    if (cVar2 != '\0') {
      cVar2 = FUN_182fc00d0(param_2,_DAT_184dfbeb8,0);
      if (cVar2 != '\0') {
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
        plVar3 = (longlong *)FUN_180361710(_DAT_184e0ac30);
        if (*plVar3 != 0) {
          uVar4 = FUN_1809700e0(*plVar3,param_2,0,0);
          *(undefined8 *)(param_1 + 0x358) = uVar4;
          FUN_180360c40(param_1 + 0x358,uVar4);
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
          plVar3 = (longlong *)FUN_180361710(_DAT_184e08f58);
          if ((*plVar3 != 0) && (lVar5 = *(longlong *)(*plVar3 + 0x30), lVar5 != 0)) {
            uVar4 = FUN_18000a5c0(6,_DAT_184e4aab8,lVar5,uVar4);
            *(undefined8 *)(param_1 + 0x360) = uVar4;
            FUN_180360c40(param_1 + 0x360,uVar4);
            *(undefined1 *)(param_1 + 0x2d8) = 1;
            return;
          }
        }
        goto LAB_180bd92fb;
      }
    }
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
    plVar3 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar3 == 0) {
      lVar5 = 0;
    }
    else {
      lVar5 = *(longlong *)(*plVar3 + 0x30);
      if (lVar5 == 0) goto LAB_180bd92fb;
      lVar5 = FUN_18000a5c0(6,_DAT_184e4aab8,lVar5,param_2);
    }
    *(longlong *)(param_1 + 0x358) = param_2;
    if (lVar5 == 0) {
      lVar5 = param_2;
    }
    FUN_180360c40(param_1 + 0x358,param_2);
    *(longlong *)(param_1 + 0x360) = lVar5;
    FUN_180360c40(param_1 + 0x360,lVar5);
    *(undefined1 *)(param_1 + 0x2d8) = 0;
    return;
  }
LAB_180bd92fb:
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}


