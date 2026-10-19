/* REA ev_51db60ff8be058f6eb7fc5e61dc843a366f73aa06c0311ec6f4bc3083a0ac7e5; source response evidence/photon-operation-response-router.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_18097d820(longlong param_1,longlong param_2,undefined2 param_3)

{
  code *pcVar1;
  undefined8 uVar2;
  char cVar3;
  undefined8 uVar4;
  longlong lVar5;
  longlong *plVar6;
  undefined8 uVar7;
  longlong lVar8;
  longlong lVar9;
  longlong *plVar10;
  undefined8 in_stack_ffffffffffffffa8;
  undefined4 uVar11;
  undefined8 local_48;
  undefined8 uStack_40;
  undefined4 local_38;
  
  uVar11 = (undefined4)((ulonglong)in_stack_ffffffffffffffa8 >> 0x20);
  if (DAT_18507ab21 == '\0') {
    FUN_180361910(&DAT_184e761f8);
    FUN_180361910(&DAT_184ddf3c0);
    FUN_180361910(&DAT_184e6aa70);
    FUN_180361910(&DAT_184e0c980);
    FUN_180361910(&DAT_184e56fc0);
    FUN_180361910(&DAT_184db6698);
    FUN_180361910(&DAT_184e44dd0);
    FUN_180361910(&DAT_184d97728);
    FUN_180361910(&DAT_184dda8b8);
    FUN_180361910(&DAT_184e49448);
    FUN_180361910(&DAT_184e08c98);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184db9160);
    FUN_180361910(&DAT_184e0ac30);
    FUN_180361910(&DAT_184e243d8);
    FUN_180361910(&DAT_184e71ad0);
    DAT_18507ab21 = '\x01';
  }
  if (param_2 != 0) {
    if (*(short *)(param_2 + 0x12) != 0) {
      uVar7 = _DAT_184d97728;
      if ((*(short *)(param_2 + 0x12) == 0x54) ||
         (uVar7 = _DAT_184e44dd0, *(short *)(param_2 + 0x12) == 0x55)) {
        *(undefined8 *)(param_1 + 0xc0) = uVar7;
        FUN_180360c40(param_1 + 0xc0);
      }
LAB_18097d96c:
      FUN_18097cae0(param_1,0);
      return;
    }
    *(undefined4 *)(param_1 + 0x14) = 0;
    cVar3 = FUN_182fc26f0(*(undefined8 *)(param_1 + 0x118),0);
    if (cVar3 == '\0') {
      if (*(int *)(_DAT_184dda8b8 + 0xe4) == 0) {
        il2cpp_runtime_class_init(_DAT_184dda8b8);
      }
      uVar7 = *(undefined8 *)(param_1 + 0x118);
      lVar5 = *(longlong *)(*(longlong *)(_DAT_184dda8b8 + 0xb8) + 0x10);
      if (*(int *)(_DAT_184ddf3c0 + 0xe4) == 0) {
        il2cpp_runtime_class_init();
      }
      uVar4 = FUN_183163e80(0);
      if (lVar5 == 0) goto LAB_18097ddef;
      FUN_182b80400(lVar5,uVar7,uVar4,_DAT_184e6aa70);
    }
    lVar5 = FUN_182004220(param_2,_DAT_184db9160);
    if (lVar5 != 0) {
      if (*(longlong *)(lVar5 + 0x218) != 0) {
        uVar7 = *(undefined8 *)(lVar5 + 0x218);
        if (*(int *)(_DAT_184e0c980 + 0xe4) == 0) {
          il2cpp_runtime_class_init();
        }
        FUN_1808b2b00(uVar7,0);
      }
      if (*(longlong *)(param_1 + 0x160) != 0) {
        cVar3 = FUN_182fc89d0(*(undefined8 *)(lVar5 + 0x60),
                              *(undefined8 *)(*(longlong *)(param_1 + 0x160) + 0x10),0);
        if (cVar3 != '\0') {
          if (*(int *)(_DAT_184e71ad0 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          FUN_1804f7650(_DAT_184db6698,0);
          goto LAB_18097d96c;
        }
        if (*(longlong *)(param_1 + 0x70) != 0) {
          FUN_180018ca0(0,_DAT_184e08c98,*(longlong *)(param_1 + 0x70),
                        *(undefined8 *)(lVar5 + 0x208),
                        CONCAT44(uVar11,*(undefined4 *)(lVar5 + 0x210)));
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
          plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
          lVar8 = *plVar6;
          if (*(int *)(_DAT_184e0c980 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          uVar7 = FUN_1808b3070(0);
          if (lVar8 != 0) {
            *(undefined8 *)(lVar8 + 0x158) = uVar7;
            if (*(longlong *)(param_1 + 400) != 0) {
              FUN_180a5b840(*(longlong *)(param_1 + 400),lVar5,0);
              if (*(char *)(lVar5 + 0x2e8) != '\0') {
                if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
                  il2cpp_runtime_class_init();
                }
                lVar8 = FUN_180956650(0);
                if (lVar8 == 0) goto LAB_18097ddef;
                *(undefined4 *)(lVar8 + 0x38) = **(undefined4 **)(_DAT_184e0ac30 + 0xb8);
              }
              if (*(longlong *)(param_1 + 0x50) != 0) {
                lVar8 = *(longlong *)(*(longlong *)(param_1 + 0x50) + 0xd8);
                local_48 = _DAT_184e56fc0;
                local_38 = *(undefined4 *)(lVar5 + 0x330);
                uStack_40 = 0xffffffffffffffff;
                uVar7 = FUN_1831ac4f0(&local_48,0);
                if (lVar8 != 0) {
                  lVar8 = FUN_18065bd70(lVar8,uVar7,0);
                  if ((lVar8 != 0) && (*(longlong *)(lVar8 + 400) != 0)) {
                    if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
                      il2cpp_runtime_class_init();
                    }
                    lVar9 = FUN_180956650(0);
                    if (lVar9 == 0) goto LAB_18097ddef;
                    FUN_18096eb10(lVar9,lVar8,*(undefined8 *)(lVar5 + 0x338),
                                  *(undefined8 *)(lVar5 + 0x340),0);
                  }
                  uVar7 = *(undefined8 *)(lVar5 + 0x20);
                  uVar11 = *(undefined4 *)(lVar5 + 0x48);
                  uVar4 = *(undefined8 *)(lVar5 + 0x28);
                  uVar2 = *(undefined8 *)(lVar5 + 0x30);
                  plVar6 = (longlong *)thunk_FUN_18039ec10(_DAT_184e243d8);
                  local_48 = uVar4;
                  uStack_40 = uVar2;
                  FUN_180990ac0(plVar6,uVar7,uVar11,&local_48,0);
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
                  if (*plVar10 != 0) {
                    FUN_18096ee00(*plVar10,plVar6,0);
                    if (DAT_18507aae8 == '\0') {
                      FUN_180361910(&DAT_184e0ac30);
                      DAT_18507aae8 = '\x01';
                    }
                    if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
                      il2cpp_runtime_class_init();
                    }
                    plVar10 = (longlong *)FUN_180361710(_DAT_184e0ac30);
                    lVar5 = *plVar10;
                    if (lVar5 != 0) {
                      *(longlong **)(lVar5 + 0x68) = plVar6;
                      FUN_180360c40(lVar5 + 0x68,plVar6);
                      if (plVar6 != (longlong *)0x0) {
                        (**(code **)(*plVar6 + 0x228))
                                  (plVar6,param_2,param_3,*(undefined8 *)(*plVar6 + 0x230));
                        lVar5 = plVar6[0x85];
                        uVar7 = *(undefined8 *)(param_1 + 400);
                        uVar4 = thunk_FUN_18039ec10(_DAT_184e761f8);
                        FUN_180429990(uVar4,uVar7,_DAT_184e49448,0);
                        if (lVar5 != 0) {
                          FUN_1807465c0(lVar5,uVar4,0);
                          return;
                        }
                      }
                    }
                  }
                  goto LAB_18097ddef;
                }
              }
              FUN_180361b60();
              pcVar1 = (code *)swi(3);
              (*pcVar1)();
              return;
            }
          }
        }
      }
    }
  }
LAB_18097ddef:
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}


