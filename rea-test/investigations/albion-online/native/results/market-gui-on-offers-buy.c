/* REA ev_78bf045a42f0cefcfaeb776949a25ede3c581fcca0810129f1560f722cc29785; source response evidence/market-gui-on-offers-buy.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180fd3820(longlong *param_1,longlong param_2)

{
  longlong *plVar1;
  longlong lVar2;
  code *pcVar3;
  longlong lVar4;
  undefined8 uVar5;
  
  if (DAT_18507e0a5 == '\0') {
    FUN_180361910(&DAT_184dc95d8);
    FUN_180361910(&DAT_184dc9730);
    FUN_180361910(&DAT_184e5ad70);
    DAT_18507e0a5 = '\x01';
  }
  if (param_2 != 0) {
    plVar1 = param_1 + 0x34;
    *(int *)plVar1 = (int)*plVar1 + -1;
    if ((int)*plVar1 < 0) {
      *(undefined4 *)(param_1 + 0x34) = 0;
    }
    if (param_1[0x28] == 0) goto LAB_180fd3a46;
    FUN_183a3ecc0(param_1[0x28],0 < (int)param_1[0x34],0);
    if ((int)param_1[0x31] != 2) {
      return;
    }
    if (*(int *)(param_2 + 0x18) < 1) {
      FUN_180fdaeb0(param_1,0,0);
      if (*(int *)(_DAT_184e5ad70 + 0xe4) == 0) {
        il2cpp_runtime_class_init(_DAT_184e5ad70);
      }
      param_1[0x33] = **(longlong **)(_DAT_184e5ad70 + 0xb8);
      if (param_1[0x26] == 0) {
LAB_180fd3a46:
        FUN_180361b60();
        pcVar3 = (code *)swi(3);
        (*pcVar3)();
        return;
      }
      FUN_183a3ecc0(param_1[0x26],0,0);
      lVar2 = param_1[0x27];
      if (lVar2 == 0) goto LAB_180fd3a46;
      if (DAT_18507e0e0 == '\0') {
        FUN_180361910(&DAT_184db1e90);
        FUN_180361910(&DAT_184db1fe8);
        FUN_180361910(&DAT_184e5c558);
        DAT_18507e0e0 = '\x01';
      }
      lVar4 = FUN_183a39e90(lVar2,0);
      if (lVar4 == 0) goto LAB_180fd3a46;
      FUN_183a3ecc0(lVar4,1,0);
      lVar4 = *(longlong *)(lVar2 + 0x20);
      if (*(int *)(_DAT_184e5c558 + 0xe4) == 0) {
        il2cpp_runtime_class_init();
      }
      uVar5 = FUN_18051eae0(_DAT_184db1e90,0);
      if (lVar4 == 0) goto LAB_180fd3a46;
      FUN_1819426a0(lVar4,uVar5,0);
      lVar2 = *(longlong *)(lVar2 + 0x28);
      uVar5 = FUN_18051eae0(_DAT_184db1fe8,0);
      if (lVar2 == 0) goto LAB_180fd3a46;
      FUN_1819426a0(lVar2,uVar5,0);
    }
    else {
      if (param_1[0x26] == 0) goto LAB_180fd3a46;
      FUN_183a3ecc0(param_1[0x26],1,0);
      uVar5 = FUN_1821939c0(param_2,0,_DAT_184dc9730);
      uVar5 = FUN_181c90250(uVar5,0,0);
      (**(code **)(*param_1 + 0x218))(param_1,uVar5,*(undefined8 *)(*param_1 + 0x220));
    }
    FUN_180fd6bc0(param_1,0);
  }
  return;
}


