/* REA ev_7fb7b0995d7fc471f6c8a8c73f7b0626922f7f0018508a15e5c6d6e718f37c88; source response evidence/equipment_indices_setter.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180bd9770(longlong param_1,longlong param_2)

{
  int iVar1;
  longlong lVar2;
  code *pcVar3;
  bool bVar4;
  uint uVar5;
  longlong lVar6;
  longlong *plVar7;
  longlong *plVar8;
  undefined8 uVar9;
  longlong *plVar10;
  longlong *plVar11;
  int *piVar12;
  longlong lVar13;
  
  if (DAT_18507bfa1 == '\0') {
    FUN_180361910(&DAT_184da6cc8);
    FUN_180361910(&DAT_184e08f58);
    DAT_18507bfa1 = '\x01';
  }
  if (param_2 != 0) {
    piVar12 = (int *)(param_2 + 0x20);
    lVar13 = 0x20;
    plVar11 = (longlong *)0x0;
    plVar7 = plVar11;
    while (uVar5 = (uint)plVar7, (int)uVar5 < *(int *)(param_2 + 0x18)) {
      if (*(uint *)(param_2 + 0x18) <= uVar5) goto LAB_180bd9a76;
      iVar1 = *piVar12;
      if (iVar1 < 0) {
        lVar6 = *(longlong *)(param_1 + 0x2c8);
        if (lVar6 == 0) goto LAB_180bd9a7c;
        if (*(uint *)(lVar6 + 0x18) <= uVar5) goto LAB_180bd9a76;
        *(undefined8 *)(lVar13 + lVar6) = 0;
        plVar10 = (longlong *)(lVar6 + (longlong)(int)uVar5 * 8 + 0x20);
        plVar7 = plVar11;
      }
      else {
        if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
          il2cpp_runtime_class_init();
        }
        lVar6 = FUN_180947b10(0);
        if (((lVar6 == 0) || (*(longlong *)(lVar6 + 0x50) == 0)) ||
           (lVar6 = *(longlong *)(*(longlong *)(lVar6 + 0x50) + 0x20), lVar6 == 0))
        goto LAB_180bd9a7c;
        plVar7 = (longlong *)FUN_180609120(lVar6,iVar1,0);
        if (plVar7 == (longlong *)0x0) {
          plVar10 = *(longlong **)(param_1 + 0x2c8);
LAB_180bd98cc:
          if (plVar10 == (longlong *)0x0) goto LAB_180bd9a7c;
          if (*(uint *)(plVar10 + 3) <= uVar5) goto LAB_180bd9a76;
          *(undefined8 *)(lVar13 + (longlong)plVar10) = 0;
          plVar7 = plVar11;
        }
        else {
          lVar6 = *plVar7;
          if ((*(byte *)(lVar6 + 0x130) < *(byte *)(_DAT_184da6cc8 + 0x130)) ||
             (bVar4 = true,
             *(longlong *)
              (*(longlong *)(lVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184da6cc8 + 0x130) * 8)
             != _DAT_184da6cc8)) {
            bVar4 = false;
          }
          plVar10 = *(longlong **)(param_1 + 0x2c8);
          plVar8 = plVar11;
          if (bVar4) {
            plVar8 = plVar7;
          }
          if (plVar8 == (longlong *)0x0) goto LAB_180bd98cc;
          if (plVar10 == (longlong *)0x0) goto LAB_180bd9a7c;
          if ((*(byte *)(lVar6 + 0x130) < *(byte *)(_DAT_184da6cc8 + 0x130)) ||
             (*(longlong *)
               (*(longlong *)(lVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184da6cc8 + 0x130) * 8)
              != _DAT_184da6cc8)) {
            FUN_180360c80(plVar7);
            pcVar3 = (code *)swi(3);
            (*pcVar3)();
            return;
          }
          lVar6 = thunk_FUN_18039da90(plVar7,*(undefined8 *)(*plVar10 + 0x40));
          if (lVar6 == 0) {
            uVar9 = thunk_FUN_180374480();
            FUN_180361b20(uVar9,0);
            pcVar3 = (code *)swi(3);
            (*pcVar3)();
            return;
          }
          if ((*(byte *)(*plVar7 + 0x130) < *(byte *)(_DAT_184da6cc8 + 0x130)) ||
             (*(longlong *)
               (*(longlong *)(*plVar7 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184da6cc8 + 0x130) * 8
               ) != _DAT_184da6cc8)) {
            FUN_180360c80(plVar7);
            pcVar3 = (code *)swi(3);
            (*pcVar3)();
            return;
          }
          if (*(uint *)(plVar10 + 3) <= uVar5) goto LAB_180bd9a76;
          *(longlong **)(lVar13 + (longlong)plVar10) = plVar7;
        }
        plVar10 = plVar10 + (longlong)(int)uVar5 + 4;
      }
      FUN_180360c40(plVar10,plVar7);
      piVar12 = piVar12 + 1;
      lVar13 = lVar13 + 8;
      plVar7 = (longlong *)(ulonglong)(uVar5 + 1);
    }
    if (DAT_18507bfa3 == '\0') {
      FUN_180361910(&DAT_184dcf8b8);
      DAT_18507bfa3 = '\x01';
    }
    lVar13 = *(longlong *)(param_1 + 0x2c8);
    *(undefined1 *)(param_1 + 0x298) = 0;
    if (lVar13 == 0) {
LAB_180bd9a7c:
      FUN_180361b60();
      pcVar3 = (code *)swi(3);
      (*pcVar3)();
      return;
    }
    plVar7 = (longlong *)(lVar13 + 0x20);
    lVar6 = _DAT_184dcf8b8;
    while (uVar5 = (uint)plVar11, (int)uVar5 < *(int *)(lVar13 + 0x18)) {
      if (*(uint *)(lVar13 + 0x18) <= uVar5) {
LAB_180bd9a76:
        FUN_180361b50();
        pcVar3 = (code *)swi(3);
        (*pcVar3)();
        return;
      }
      if ((longlong *)*plVar7 != (longlong *)0x0) {
        lVar2 = *(longlong *)*plVar7;
        if ((*(byte *)(lVar6 + 0x130) <= *(byte *)(lVar2 + 0x130)) &&
           (*(longlong *)(*(longlong *)(lVar2 + 200) + -8 + (ulonglong)*(byte *)(lVar6 + 0x130) * 8)
            == lVar6)) {
          *(undefined1 *)(param_1 + 0x298) = 1;
          lVar6 = _DAT_184dcf8b8;
        }
      }
      plVar7 = plVar7 + 1;
      plVar11 = (longlong *)(ulonglong)(uVar5 + 1);
    }
  }
  return;
}


