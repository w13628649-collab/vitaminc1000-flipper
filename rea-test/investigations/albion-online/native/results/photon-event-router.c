/* REA ev_13c394eb5f8aab059323af5d95a83177413e5b29b72fd1f6638d829a269efe55; source response evidence/photon-event-router.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_18097f270(undefined8 param_1,undefined8 param_2,undefined2 param_3)

{
  undefined1 uVar1;
  longlong lVar2;
  code *pcVar3;
  undefined1 auVar4 [16];
  bool bVar5;
  char cVar6;
  char cVar7;
  longlong lVar8;
  longlong *plVar9;
  longlong lVar10;
  longlong lVar11;
  longlong lVar12;
  undefined8 uVar13;
  undefined8 *puVar14;
  undefined8 uVar15;
  longlong *plVar16;
  undefined4 uVar17;
  undefined4 uVar18;
  undefined4 uVar19;
  undefined4 uVar20;
  undefined8 local_48;
  undefined8 uStack_40;
  
  if (DAT_18507ab45 == '\0') {
    FUN_180361910(&DAT_184e0deb0);
    FUN_180361910(&DAT_184e0dd50);
    FUN_180361910(&DAT_184e0e010);
    FUN_180361910(&DAT_184de9c60);
    FUN_180361910(&DAT_184de9dc0);
    FUN_180361910(&DAT_184e65980);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184dc2ff0);
    FUN_180361910(&DAT_184e0ac30);
    FUN_180361910(&DAT_184e1e768);
    DAT_18507ab45 = '\x01';
  }
  lVar8 = FUN_182003eb0(param_2,_DAT_184dc2ff0);
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
    plVar9 = (longlong *)FUN_18096f090(*plVar9,*(undefined8 *)(lVar8 + 0x18),0);
    if (plVar9 != (longlong *)0x0) {
      (**(code **)(*plVar9 + 0x238))(plVar9,param_2,param_3,*(undefined8 *)(*plVar9 + 0x240));
    }
    lVar10 = thunk_FUN_18039ec10(_DAT_184de9c60);
    FUN_180429690(lVar10,0);
    if (lVar10 != 0) {
      *(undefined8 *)(lVar10 + 0x10) = *(undefined8 *)(lVar8 + 0x18);
      *(undefined8 *)(lVar10 + 0x28) = *(undefined8 *)(lVar8 + 0x20);
      FUN_180360c40(lVar10 + 0x28);
      *(undefined8 *)(lVar10 + 0x30) = *(undefined8 *)(lVar8 + 0x28);
      FUN_180360c40(lVar10 + 0x30);
      uVar13 = *(undefined8 *)(lVar8 + 0x38);
      *(undefined8 *)(lVar10 + 0x38) = *(undefined8 *)(lVar8 + 0x30);
      *(undefined8 *)(lVar10 + 0x40) = uVar13;
      *(undefined1 *)(lVar10 + 0x48) = *(undefined1 *)(lVar8 + 0x40);
      *(undefined1 *)(lVar10 + 0x50) = *(undefined1 *)(lVar8 + 0x41);
      *(undefined4 *)(lVar10 + 0x4c) = *(undefined4 *)(lVar8 + 0x44);
      lVar11 = thunk_FUN_18039ec10(_DAT_184de9c60);
      FUN_180429690(lVar11,0);
      if (lVar11 != 0) {
        *(undefined8 *)(lVar11 + 0x10) = *(undefined8 *)(lVar8 + 0x50);
        *(undefined8 *)(lVar11 + 0x28) = *(undefined8 *)(lVar8 + 0x58);
        FUN_180360c40(lVar11 + 0x28);
        *(undefined8 *)(lVar11 + 0x30) = *(undefined8 *)(lVar8 + 0x60);
        FUN_180360c40(lVar11 + 0x30);
        uVar13 = *(undefined8 *)(lVar8 + 0x70);
        *(undefined8 *)(lVar11 + 0x38) = *(undefined8 *)(lVar8 + 0x68);
        *(undefined8 *)(lVar11 + 0x40) = uVar13;
        *(undefined1 *)(lVar11 + 0x48) = *(undefined1 *)(lVar8 + 0x78);
        *(undefined1 *)(lVar11 + 0x50) = *(undefined1 *)(lVar8 + 0x79);
        *(undefined4 *)(lVar11 + 0x4c) = *(undefined4 *)(lVar8 + 0x7c);
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
        if (*plVar9 != 0) {
          cVar6 = FUN_180973590(*plVar9,0);
          if (DAT_18507aae8 == '\0') {
            FUN_180361910(&DAT_184e0ac30);
            DAT_18507aae8 = '\x01';
          }
          if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          plVar9 = (longlong *)FUN_180361710(_DAT_184e0ac30);
          if (*plVar9 != 0) {
            cVar7 = FUN_1809734d0(*plVar9,0);
            lVar12 = *(longlong *)(lVar8 + 0x18);
            if (DAT_18507aae8 == '\0') {
              FUN_180361910(&DAT_184e0ac30);
              DAT_18507aae8 = '\x01';
            }
            if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
              il2cpp_runtime_class_init();
            }
            plVar9 = (longlong *)FUN_180361710(_DAT_184e0ac30);
            if (*plVar9 != 0) {
              lVar2 = *(longlong *)(*plVar9 + 0x68);
              if (lVar2 == 0) {
                uVar17 = 0;
                uVar18 = 0;
                uVar19 = 0;
                uVar20 = 0;
              }
              else {
                local_48 = 0;
                uStack_40 = 0;
                FUN_1823b4670(&local_48,*(undefined8 *)(lVar2 + 0x10),_DAT_184e0dd50);
                uVar17 = (undefined4)local_48;
                uVar18 = local_48._4_4_;
                uVar19 = (undefined4)uStack_40;
                uVar20 = uStack_40._4_4_;
              }
              auVar4._4_4_ = uVar18;
              auVar4._0_4_ = uVar17;
              auVar4._8_4_ = uVar19;
              auVar4._12_4_ = uVar20;
              if ((((byte)uVar17 & lVar12 == auVar4._8_8_) == 0) && (cVar7 != '\0' || cVar6 != '\0')
                 ) {
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
                if (*plVar9 == 0) goto LAB_18097f855;
                plVar9 = (longlong *)FUN_18096f090(*plVar9,*(undefined8 *)(lVar11 + 0x10),0);
                if (plVar9 == (longlong *)0x0) {
                  plVar16 = (longlong *)0x0;
                }
                else {
                  if ((*(byte *)(*plVar9 + 0x130) < *(byte *)(_DAT_184e1e768 + 0x130)) ||
                     (*(longlong *)
                       (*(longlong *)(*plVar9 + 200) + -8 +
                       (ulonglong)*(byte *)(_DAT_184e1e768 + 0x130) * 8) != _DAT_184e1e768)) {
                    bVar5 = false;
                  }
                  else {
                    bVar5 = true;
                  }
                  plVar16 = (longlong *)0x0;
                  if (bVar5) {
                    plVar16 = plVar9;
                  }
                }
                if (cVar6 != '\0') {
                  if (plVar16 == (longlong *)0x0) {
                    if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
                      il2cpp_runtime_class_init();
                    }
                    lVar12 = FUN_180956650(0);
                    if (lVar12 == 0) goto LAB_18097f855;
                    uVar13 = FUN_1809700e0(lVar12,*(undefined8 *)(lVar11 + 0x28),0,0);
                  }
                  else {
                    uVar13 = (**(code **)(*plVar16 + 0x2a8))
                                       (plVar16,*(undefined8 *)(*plVar16 + 0x2b0));
                  }
                  *(undefined8 *)(lVar11 + 0x28) = uVar13;
                  FUN_180360c40(lVar11 + 0x28,uVar13);
                }
                if ((cVar7 != '\0') && (plVar16 != (longlong *)0x0)) {
                  uVar13 = (**(code **)(*plVar16 + 0x5e8))
                                     (plVar16,*(undefined8 *)(*plVar16 + 0x5f0));
                  *(undefined8 *)(lVar11 + 0x30) = uVar13;
                  FUN_180360c40(lVar11 + 0x30,uVar13);
                  puVar14 = (undefined8 *)
                            (**(code **)(*plVar16 + 0x648))
                                      (&local_48,plVar16,*(undefined8 *)(*plVar16 + 0x650));
                  uVar13 = puVar14[1];
                  *(undefined8 *)(lVar11 + 0x38) = *puVar14;
                  *(undefined8 *)(lVar11 + 0x40) = uVar13;
                }
              }
              uVar13 = *(undefined8 *)(lVar8 + 0x10);
              uVar1 = *(undefined1 *)(lVar8 + 0x48);
              uVar15 = thunk_FUN_18039ec10(_DAT_184de9dc0);
              FUN_180a99760(uVar15,uVar13,lVar10,lVar11,uVar1,0);
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
              if ((*plVar9 != 0) && (lVar8 = *(longlong *)(*plVar9 + 0x228), lVar8 != 0)) {
                FUN_182003b60(lVar8,uVar15,0,_DAT_184e65980);
                return;
              }
            }
          }
        }
      }
    }
  }
LAB_18097f855:
  FUN_180361b60();
  pcVar3 = (code *)swi(3);
  (*pcVar3)();
  return;
}


