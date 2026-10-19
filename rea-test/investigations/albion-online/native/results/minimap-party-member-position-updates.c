/* REA ev_a2e8f7a470169f9315f5f94e1ae1b18763fb01a06b5ab2ea88c78e3ee72f5325; source response evidence/minimap-party-member-position-updates.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_18145c270(longlong *param_1,undefined8 param_2)

{
  longlong lVar1;
  code *pcVar2;
  char cVar3;
  char cVar4;
  longlong *plVar5;
  longlong lVar6;
  longlong *plVar7;
  undefined8 uVar8;
  undefined8 uVar9;
  longlong lVar10;
  longlong lVar11;
  undefined8 local_148;
  undefined8 *local_140;
  longlong local_138 [2];
  undefined8 *local_128;
  longlong local_120;
  longlong local_118;
  undefined8 local_110;
  undefined8 uStack_108;
  longlong local_100;
  longlong local_f8;
  longlong lStack_f0;
  longlong local_e8;
  undefined8 local_d8;
  undefined8 uStack_d0;
  longlong local_c8;
  longlong lStack_c0;
  undefined8 local_b8;
  undefined4 local_b0;
  undefined4 uStack_ac;
  undefined4 uStack_a8;
  undefined4 uStack_a4;
  longlong local_a0;
  longlong lStack_98;
  undefined8 local_90;
  undefined8 local_88;
  undefined8 uStack_80;
  longlong local_78;
  
  if (DAT_1850803ff == '\0') {
    FUN_180361910(&DAT_184e11948);
    FUN_180361910(&DAT_184e11aa8);
    FUN_180361910(&DAT_184e11d68);
    FUN_180361910(&DAT_184e11ec8);
    FUN_180361910(&DAT_184e12020);
    FUN_180361910(&DAT_184e122d8);
    FUN_180361910(&DAT_184dda0b8);
    FUN_180361910(&DAT_184de0b38);
    FUN_180361910(&DAT_184e3bad8);
    FUN_180361910(&DAT_184dda210);
    FUN_180361910(&DAT_184e3bc30);
    FUN_180361910(&DAT_184de0c98);
    FUN_180361910(&DAT_184de0df8);
    FUN_180361910(&DAT_184e3bd90);
    FUN_180361910(&DAT_184dda368);
    FUN_180361910(&DAT_184e0be80);
    FUN_180361910(&DAT_184dd0de0);
    FUN_180361910(&DAT_184dd0f38);
    FUN_180361910(&DAT_184da9b60);
    FUN_180361910(&DAT_184daa200);
    FUN_180361910(&DAT_184dad788);
    FUN_180361910(&DAT_184da9760);
    FUN_180361910(&DAT_184e58938);
    FUN_180361910(&DAT_184e632c8);
    FUN_180361910(&DAT_184de0250);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184e0ac30);
    FUN_180361910(&DAT_184e1e768);
    FUN_180361910(&DAT_184e71ad0);
    DAT_1850803ff = '\x01';
  }
  local_110 = 0;
  uStack_108 = 0;
  local_100 = 0;
  lVar10 = 0;
  local_138[0] = 0;
  local_d8 = 0;
  uStack_d0 = 0;
  local_c8 = 0;
  lStack_c0 = 0;
  local_b8 = 0;
  local_88 = 0;
  uStack_80 = 0;
  local_78 = 0;
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
  plVar5 = (longlong *)FUN_180361710(_DAT_184e0ac30);
  if (*plVar5 == 0) {
    return;
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
  plVar5 = (longlong *)FUN_180361710(_DAT_184e08f58);
  if (*plVar5 == 0) {
    return;
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
  plVar5 = (longlong *)FUN_180361710(_DAT_184e0ac30);
  if (*plVar5 != 0) {
    cVar3 = FUN_182fc00d0(param_2,*(undefined8 *)(*plVar5 + 0x10),0);
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
    plVar5 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar5 != 0) {
      lVar1 = *(longlong *)(*plVar5 + 400);
      if (lVar1 != 0) {
        lVar1 = *(longlong *)(lVar1 + 0x58);
        cVar4 = '\0';
        if (lVar1 != 0) {
          cVar4 = cVar3;
        }
        local_120 = lVar1;
        if (cVar4 == '\0') {
          if (lVar1 == 0) goto LAB_18145cc9e;
        }
        else {
          if ((lVar1 == 0) || (*(longlong *)(lVar1 + 0x30) == 0)) goto LAB_18145ce33;
          FUN_18218a540(&local_f8,*(longlong *)(lVar1 + 0x30),_DAT_184dad788);
          local_110 = local_f8;
          uStack_108 = lStack_f0;
          local_100 = local_e8;
          local_138[1] = 0;
          local_128 = &local_110;
          while (cVar4 = FUN_182c89df0(&local_110,_DAT_184e3bc30), lVar11 = local_100, cVar4 != '\0'
                ) {
            if (local_100 == 0) {
              if (*(int *)(_DAT_184e71ad0 + 0xe4) == 0) {
                il2cpp_runtime_class_init();
              }
              FUN_1804f7650(_DAT_184de0250,0);
            }
            else {
              uVar8 = *(undefined8 *)(local_100 + 0x48);
              if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
                il2cpp_runtime_class_init();
              }
              lVar6 = FUN_180956650(0);
              if (lVar6 == 0) goto LAB_18145ce58;
              plVar5 = (longlong *)FUN_18096f090(lVar6,uVar8,0);
              if (plVar5 != (longlong *)0x0) {
                if ((*(byte *)(_DAT_184e1e768 + 0x130) <= *(byte *)(*plVar5 + 0x130)) &&
                   (*(longlong *)
                     (*(longlong *)(*plVar5 + 200) + -8 +
                     (ulonglong)*(byte *)(_DAT_184e1e768 + 0x130) * 8) == _DAT_184e1e768)) {
                  if (*(int *)(_DAT_184e632c8 + 0xe4) == 0) {
                    il2cpp_runtime_class_init();
                  }
                  plVar7 = (longlong *)FUN_18132d600(&local_148,lVar11,1,0);
                  lVar11 = *plVar7;
                  lVar6 = plVar7[1];
                  if (param_1[0x1c] == 0) goto LAB_18145ce53;
                  cVar4 = FUN_182b2b4c0(param_1[0x1c],uVar8,local_138,_DAT_184e12020);
                  if (cVar4 == '\0') {
                    local_138[0] = (**(code **)(*param_1 + 0x3b8))
                                             (param_1,*(undefined8 *)(*param_1 + 0x3c0));
                    if (local_138[0] == 0) goto LAB_18145ce4e;
                    local_f8 = lVar11;
                    lStack_f0 = lVar6;
                    FUN_18132c8b0(local_138[0],param_1,uVar8,&local_f8,0);
                    lVar11 = local_138[0];
                    uVar9 = (**(code **)(*plVar5 + 0x268))(plVar5,*(undefined8 *)(*plVar5 + 0x270));
                    if (lVar11 == 0) goto LAB_18145ce49;
                    FUN_18132f150(lVar11,uVar9,0);
                    if (param_1[0x1c] == 0) goto LAB_18145ce44;
                    FUN_182b1f2c0(param_1[0x1c],uVar8);
                  }
                  else {
                    if (local_138[0] == 0) goto LAB_18145ce3f;
                    local_f8 = lVar11;
                    lStack_f0 = lVar6;
                    FUN_18132c8b0(local_138[0],param_1,uVar8,&local_f8,0);
                    lVar11 = local_138[0];
                    uVar8 = (**(code **)(*plVar5 + 0x268))(plVar5,*(undefined8 *)(*plVar5 + 0x270));
                    if (lVar11 == 0) goto LAB_18145ce39;
                    FUN_18132e490(lVar11,uVar8,0x3dcccccd,1,0);
                  }
                }
              }
            }
          }
          FUN_1804873c0(&local_110,_DAT_184e3bad8);
        }
        if (cVar3 != '\0') {
          local_118 = 0;
          if (param_1[0x1c] != 0) {
            FUN_182a48ea0(&local_b0,param_1[0x1c],_DAT_184e11d68);
            local_d8 = CONCAT44(uStack_ac,local_b0);
            uStack_d0 = CONCAT44(uStack_a4,uStack_a8);
            local_c8 = local_a0;
            lStack_c0 = lStack_98;
            local_b8 = local_90;
            local_138[1] = 0;
            local_128 = &local_d8;
            while (cVar3 = FUN_182cbd010(&local_d8,_DAT_184de0c98), cVar3 != '\0') {
              local_f8 = local_c8;
              lStack_f0 = lStack_c0;
              lVar11 = local_c8;
              if (lVar1 == 0) {
LAB_18145caa2:
                if (lVar10 == 0) {
                  lVar10 = thunk_FUN_18039ec10(_DAT_184e58938);
                  FUN_1821861f0(lVar10,_DAT_184da9760);
                  local_118 = lVar10;
                  if (lVar10 == 0) goto LAB_18145ce6f;
                }
                FUN_18000c440(lVar10,lVar11,_DAT_184da9b60);
              }
              else {
                if (*(longlong *)(lVar1 + 0x30) == 0) goto LAB_18145ce74;
                FUN_18218a540(&local_b0,*(longlong *)(lVar1 + 0x30),_DAT_184dad788);
                lVar11 = local_f8;
                local_110 = CONCAT44(uStack_ac,local_b0);
                uStack_108 = CONCAT44(uStack_a4,uStack_a8);
                local_100 = local_a0;
                local_148 = 0;
                local_140 = &local_110;
                do {
                  cVar3 = FUN_182c89df0(&local_110,_DAT_184e3bc30);
                  if (cVar3 == '\0') {
                    FUN_1804873c0(&local_110,_DAT_184e3bad8);
                    goto LAB_18145caa2;
                  }
                  if (local_100 == 0) goto LAB_18145ce64;
                } while (*(longlong *)(local_100 + 0x48) != lVar11);
                FUN_1804873c0(&local_110,_DAT_184e3bad8);
              }
            }
            FUN_1804873c0(local_128,_DAT_184de0b38);
            if (lVar10 == 0) {
              return;
            }
            FUN_18218a540(&local_b0,lVar10,_DAT_184daa200);
            local_88 = CONCAT44(uStack_ac,local_b0);
            uStack_80 = CONCAT44(uStack_a4,uStack_a8);
            local_78 = local_a0;
            local_148 = 0;
            local_140 = &local_88;
            while( true ) {
              cVar3 = FUN_182c744e0(&local_88,_DAT_184dda210);
              lVar10 = local_78;
              if (cVar3 == '\0') {
                FUN_1804873c0(&local_88,_DAT_184dda0b8);
                return;
              }
              if (param_1[0x1c] == 0) break;
              uVar8 = FUN_182b2fa50(param_1[0x1c],local_78,_DAT_184e122d8);
              if (param_1[0x1c] == 0) goto LAB_18145ce8a;
              FUN_182b248b0(param_1[0x1c],lVar10,_DAT_184e11ec8);
              if (*(int *)(_DAT_184e0be80 + 0xe4) == 0) {
                il2cpp_runtime_class_init();
              }
              if (DAT_18507c3f6 == '\0') {
                FUN_180361910(&DAT_184e0be80);
                DAT_18507c3f6 = '\x01';
              }
              if (*(int *)(_DAT_184e0be80 + 0xe4) == 0) {
                il2cpp_runtime_class_init();
              }
              lVar10 = *(longlong *)(*(longlong *)(_DAT_184e0be80 + 0xb8) + 8);
              if (lVar10 == 0) goto LAB_18145ce85;
              lVar10 = *(longlong *)(lVar10 + 0x140);
              if (lVar10 == 0) goto LAB_18145ce80;
              FUN_18176e6b0(lVar10,uVar8,0);
            }
            goto LAB_18145ce8f;
          }
          goto LAB_18145ce33;
        }
      }
LAB_18145cc9e:
      if (param_1[0x1c] != 0) {
        FUN_182a48ea0(&local_b0,param_1[0x1c],_DAT_184e11d68);
        local_d8 = CONCAT44(uStack_ac,local_b0);
        uStack_d0 = CONCAT44(uStack_a4,uStack_a8);
        local_c8 = local_a0;
        lStack_c0 = lStack_98;
        local_b8 = local_90;
        local_148 = 0;
        local_140 = &local_d8;
        while (cVar3 = FUN_182cbd010(&local_d8,_DAT_184de0c98), lVar10 = lStack_c0, cVar3 != '\0') {
          if (*(int *)(_DAT_184e0be80 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          if (DAT_18507c3f6 == '\0') {
            FUN_180361910(&DAT_184e0be80);
            DAT_18507c3f6 = '\x01';
          }
          if (*(int *)(_DAT_184e0be80 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          lVar1 = *(longlong *)(*(longlong *)(_DAT_184e0be80 + 0xb8) + 8);
          if (lVar1 == 0) goto LAB_18145cea0;
          lVar1 = *(longlong *)(lVar1 + 0x140);
          if (lVar1 == 0) goto LAB_18145ce9b;
          FUN_18176e6b0(lVar1,lVar10,0);
        }
        FUN_1804873c0(&local_d8,_DAT_184de0b38);
        if (param_1[0x1c] != 0) {
          FUN_182a46990(param_1[0x1c],_DAT_184e11aa8);
          return;
        }
      }
    }
  }
LAB_18145ce33:
  FUN_180361b60();
LAB_18145ce39:
  FUN_180361b60();
LAB_18145ce3f:
  FUN_180361b60();
LAB_18145ce44:
  FUN_180361b60();
LAB_18145ce49:
  FUN_180361b60();
LAB_18145ce4e:
  FUN_180361b60();
LAB_18145ce53:
  FUN_180361b60();
LAB_18145ce58:
  FUN_180361b60();
  FUN_18035da00();
LAB_18145ce64:
  FUN_180361b60();
  FUN_18035da00();
LAB_18145ce6f:
  FUN_180361b60();
LAB_18145ce74:
  FUN_180361b60();
  FUN_18035da00();
LAB_18145ce80:
  FUN_180361b60();
LAB_18145ce85:
  FUN_180361b60();
LAB_18145ce8a:
  FUN_180361b60();
LAB_18145ce8f:
  FUN_180361b60();
  FUN_18035da00();
LAB_18145ce9b:
  FUN_180361b60();
LAB_18145cea0:
  FUN_180361b60();
  FUN_18035da00();
  pcVar2 = (code *)swi(3);
  (*pcVar2)();
  return;
}


