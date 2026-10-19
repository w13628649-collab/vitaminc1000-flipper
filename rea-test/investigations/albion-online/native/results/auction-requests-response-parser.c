/* REA ev_86195ba6ed6c07b982a5701b9a0929720c6a2b007e4db6f70733c677ce2c699c; source response evidence/auction-requests-response-parser.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180b27340(undefined8 param_1,undefined8 param_2)

{
  undefined1 uVar1;
  undefined2 uVar2;
  uint uVar3;
  longlong lVar4;
  longlong lVar5;
  code *pcVar6;
  longlong lVar7;
  longlong lVar8;
  undefined8 uVar9;
  longlong *plVar10;
  undefined8 uVar11;
  uint uVar12;
  undefined8 *puVar13;
  
  if (DAT_18507b8aa == '\0') {
    FUN_180361910(&DAT_184dc8dc8);
    FUN_180361910(&DAT_184dc8c70);
    FUN_180361910(&DAT_184da21e8);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184dae640);
    FUN_180361910(&DAT_184e4bee0);
    DAT_18507b8aa = '\x01';
  }
  lVar7 = FUN_182004220(param_2,_DAT_184dae640);
  lVar8 = thunk_FUN_18039ec10(_DAT_184da21e8);
  FUN_1821861f0(lVar8,_DAT_184dc8c70);
  if (lVar7 != 0) {
    if (*(longlong *)(lVar7 + 0x20) != 0) {
      lVar4 = *(longlong *)(lVar7 + 0x20);
      uVar12 = 0;
      puVar13 = (undefined8 *)(lVar4 + 0x20);
      while ((int)uVar12 < *(int *)(lVar4 + 0x18)) {
        if (*(uint *)(lVar4 + 0x18) <= uVar12) {
LAB_180b2764e:
          FUN_180361b50();
          pcVar6 = (code *)swi(3);
          (*pcVar6)();
          return;
        }
        uVar11 = *puVar13;
        uVar9 = thunk_FUN_18039ec10(_DAT_184e4bee0);
        FUN_180b20e70(uVar9,uVar11);
        if (lVar8 == 0) goto LAB_180b27648;
        *(int *)(lVar8 + 0x1c) = *(int *)(lVar8 + 0x1c) + 1;
        lVar5 = *(longlong *)(lVar8 + 0x10);
        if (lVar5 == 0) goto LAB_180b27648;
        uVar3 = *(uint *)(lVar8 + 0x18);
        if (uVar3 < *(uint *)(lVar5 + 0x18)) {
          *(uint *)(lVar8 + 0x18) = uVar3 + 1;
          if (*(uint *)(lVar5 + 0x18) <= uVar3) goto LAB_180b2764e;
          *(undefined8 *)(lVar5 + 0x20 + (longlong)(int)uVar3 * 8) = uVar9;
          FUN_180360c40(lVar5 + 0x20 + (longlong)(int)uVar3 * 8,uVar9);
          uVar12 = uVar12 + 1;
          puVar13 = puVar13 + 1;
        }
        else {
          FUN_182252760(lVar8,uVar9);
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
    plVar10 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar10 != 0) {
      lVar4 = *(longlong *)(*plVar10 + 0x1b0);
      uVar1 = *(undefined1 *)(lVar7 + 0x28);
      uVar2 = *(undefined2 *)(lVar7 + 0x10);
      if (lVar4 != 0) {
        if (DAT_18507b89b == '\0') {
          FUN_180361910(&DAT_184da1528);
          FUN_180361910(&DAT_184e08f58);
          FUN_180361910(&DAT_184e446c0);
          DAT_18507b89b = '\x01';
        }
        uVar11 = FUN_180b224c0(lVar4,lVar8,0);
        *(undefined8 *)(lVar4 + 0x68) = uVar11;
        FUN_180360c40(lVar4 + 0x68,uVar11);
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
        plVar10 = (longlong *)FUN_180361710(_DAT_184e08f58);
        if (*plVar10 != 0) {
          lVar7 = *(longlong *)(*plVar10 + 0x228);
          lVar8 = thunk_FUN_18039ec10(_DAT_184e446c0);
          FUN_180a982b0(lVar8,uVar2,0);
          if ((lVar8 != 0) && (*(undefined1 *)(lVar8 + 0x20) = uVar1, lVar7 != 0)) {
            FUN_182003b60(lVar7,lVar8,0,_DAT_184da1528);
            return;
          }
        }
      }
    }
  }
LAB_180b27648:
  FUN_180361b60();
  pcVar6 = (code *)swi(3);
  (*pcVar6)();
  return;
}


