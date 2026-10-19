/* REA ev_d852ad07d58d9aa6f03c8d4c591b3ac3015540e58fe6969bdbba33bacf5da7fc; source response evidence/auction-offers-response-parser.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180b27010(undefined8 param_1,undefined8 param_2)

{
  undefined1 uVar1;
  undefined2 uVar2;
  uint uVar3;
  longlong lVar4;
  code *pcVar5;
  longlong lVar6;
  longlong lVar7;
  undefined8 uVar8;
  longlong *plVar9;
  undefined8 uVar10;
  longlong lVar11;
  uint uVar12;
  undefined8 *puVar13;
  
  if (DAT_18507b8a9 == '\0') {
    FUN_180361910(&DAT_184dc8dc8);
    FUN_180361910(&DAT_184dc8c70);
    FUN_180361910(&DAT_184da21e8);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184dae4e8);
    FUN_180361910(&DAT_184e4bee0);
    DAT_18507b8a9 = '\x01';
  }
  lVar6 = FUN_182004220(param_2,_DAT_184dae4e8);
  lVar7 = thunk_FUN_18039ec10(_DAT_184da21e8);
  FUN_1821861f0(lVar7,_DAT_184dc8c70);
  if (lVar6 != 0) {
    if (*(longlong *)(lVar6 + 0x20) != 0) {
      lVar11 = *(longlong *)(lVar6 + 0x20);
      uVar12 = 0;
      puVar13 = (undefined8 *)(lVar11 + 0x20);
      while ((int)uVar12 < *(int *)(lVar11 + 0x18)) {
        if (*(uint *)(lVar11 + 0x18) <= uVar12) {
LAB_180b27336:
          FUN_180361b50();
          pcVar5 = (code *)swi(3);
          (*pcVar5)();
          return;
        }
        uVar10 = *puVar13;
        uVar8 = thunk_FUN_18039ec10(_DAT_184e4bee0);
        FUN_180b20e70(uVar8,uVar10);
        if (lVar7 == 0) goto LAB_180b27330;
        *(int *)(lVar7 + 0x1c) = *(int *)(lVar7 + 0x1c) + 1;
        lVar4 = *(longlong *)(lVar7 + 0x10);
        if (lVar4 == 0) goto LAB_180b27330;
        uVar3 = *(uint *)(lVar7 + 0x18);
        if (uVar3 < *(uint *)(lVar4 + 0x18)) {
          *(uint *)(lVar7 + 0x18) = uVar3 + 1;
          if (*(uint *)(lVar4 + 0x18) <= uVar3) goto LAB_180b27336;
          *(undefined8 *)(lVar4 + 0x20 + (longlong)(int)uVar3 * 8) = uVar8;
          FUN_180360c40(lVar4 + 0x20 + (longlong)(int)uVar3 * 8,uVar8);
          uVar12 = uVar12 + 1;
          puVar13 = puVar13 + 1;
        }
        else {
          FUN_182252760(lVar7,uVar8);
          uVar12 = uVar12 + 1;
          puVar13 = puVar13 + 1;
        }
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
    plVar9 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar9 != 0) {
      lVar11 = *(longlong *)(*plVar9 + 0x1b0);
      uVar1 = *(undefined1 *)(lVar6 + 0x28);
      uVar2 = *(undefined2 *)(lVar6 + 0x10);
      if (lVar11 != 0) {
        if (DAT_18507b895 == '\0') {
          FUN_180361910(&DAT_184dc95d8);
          FUN_180361910(&DAT_184da0e70);
          FUN_180361910(&DAT_184e08f58);
          FUN_180361910(&DAT_184e43ff8);
          DAT_18507b895 = '\x01';
        }
        uVar10 = FUN_180b224c0(lVar11,lVar7,0);
        *(undefined8 *)(lVar11 + 0x68) = uVar10;
        FUN_180360c40(lVar11 + 0x68,uVar10);
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
        if (*plVar9 != 0) {
          lVar6 = *(longlong *)(*plVar9 + 0x228);
          lVar11 = thunk_FUN_18039ec10(_DAT_184e43ff8);
          FUN_180a982b0(lVar11,uVar2,0);
          if ((lVar7 != 0) && (lVar11 != 0)) {
            *(undefined4 *)(lVar11 + 0x20) = *(undefined4 *)(lVar7 + 0x18);
            *(undefined1 *)(lVar11 + 0x24) = uVar1;
            if (lVar6 != 0) {
              FUN_182003b60(lVar6,lVar11,0,_DAT_184da0e70);
              return;
            }
          }
        }
      }
    }
  }
LAB_180b27330:
  FUN_180361b60();
  pcVar5 = (code *)swi(3);
  (*pcVar5)();
  return;
}


