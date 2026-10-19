/* REA ev_35f8f41ad156a7f52ee6b16106e0b71dfce707a6b1ce9b55478e19a152958aa0; source response evidence/worldmap-region-mesh.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

undefined8 FUN_180e9b6c0(longlong param_1,longlong param_2)

{
  code *pcVar1;
  undefined8 uVar2;
  undefined4 uVar3;
  longlong lVar4;
  undefined8 uVar5;
  longlong lVar6;
  longlong lVar7;
  undefined8 *puVar8;
  longlong *plVar9;
  uint uVar10;
  int iVar11;
  float fVar12;
  float fVar13;
  float local_res20;
  float fStackX_24;
  undefined8 local_a8;
  undefined4 local_a0;
  undefined8 local_90;
  undefined4 uStack_88;
  undefined4 uStack_84;
  undefined4 uStack_80;
  undefined4 uStack_7c;
  undefined1 local_78 [96];
  
  if (DAT_18507d65c == '\0') {
    FUN_180361910(&DAT_184e254c0);
    FUN_180361910(&DAT_184e14360);
    FUN_180361910(&DAT_184e4b978);
    FUN_180361910(&DAT_184db8ae8);
    FUN_180361910(&DAT_184e272d8);
    FUN_180361910(&DAT_184d9e780);
    FUN_180361910(&DAT_184e48630);
    DAT_18507d65c = '\x01';
  }
  lVar4 = thunk_FUN_18039ec10(_DAT_184e48630);
  FUN_1804873c0(lVar4,0);
  if (lVar4 != 0) {
    *(longlong *)(lVar4 + 0x10) = param_1;
    FUN_180360c40(lVar4 + 0x10,param_1);
    if (*(longlong *)(param_1 + 0x30) != 0) {
      if (*(longlong *)(*(longlong *)(param_1 + 0x30) + 0x18) == 0) {
        return 0;
      }
      if (param_2 != 0) {
        uVar5 = *(undefined8 *)(param_2 + 400);
        uVar2 = *(undefined8 *)(param_2 + 0x188);
        if (*(int *)(_DAT_184e272d8 + 0xe4) == 0) {
          il2cpp_runtime_class_init();
        }
        uVar5 = FUN_180509870(uVar5,uVar2,0);
        uVar5 = FUN_180d96920(uVar5,0);
        local_res20 = (float)uVar5;
        *(float *)(lVar4 + 0x18) = local_res20 * 0.5 * 0.01 * 1.2;
        fStackX_24 = (float)((ulonglong)uVar5 >> 0x20);
        *(float *)(lVar4 + 0x1c) = fStackX_24 * 0.5 * 0.01 * 1.2;
        if (*(longlong *)(param_1 + 0x30) != 0) {
          lVar6 = FUN_18067bdf0(*(longlong *)(param_1 + 0x30),0);
          uVar5 = _DAT_184e4b978;
          if (lVar6 == 0) {
            lVar7 = 0;
          }
          else {
            lVar7 = thunk_FUN_18039da90(lVar6,_DAT_184e4b978);
            if (lVar7 == 0) {
              FUN_180360c80(lVar6,uVar5);
              pcVar1 = (code *)swi(3);
              uVar5 = (*pcVar1)();
              return uVar5;
            }
          }
          uVar5 = thunk_FUN_18039ec10(_DAT_184e14360);
          FUN_1829f6c80(uVar5,lVar4,_DAT_184d9e780,0);
          FUN_181ddfb00(lVar7,uVar5,_DAT_184e254c0);
          if (lVar7 != 0) {
            if (*(int *)(lVar7 + 0x18) == 0) {
LAB_180e9ba8a:
              FUN_180361b50();
              pcVar1 = (code *)swi(3);
              uVar5 = (*pcVar1)();
              return uVar5;
            }
            if (*(longlong *)(lVar7 + 0x20) != 0) {
              puVar8 = (undefined8 *)FUN_183a14570(&local_a8,*(longlong *)(lVar7 + 0x20),0);
              local_90 = *puVar8;
              uStack_80 = (undefined4)puVar8[2];
              uStack_7c = (undefined4)((ulonglong)puVar8[2] >> 0x20);
              uStack_88 = (undefined4)puVar8[1];
              uStack_84 = (undefined4)((ulonglong)puVar8[1] >> 0x20);
              local_a0 = uStack_7c;
              local_a8 = CONCAT44(uStack_80,uStack_84);
              fVar12 = (float)FUN_1804b5710(&local_a8,0);
              plVar9 = (longlong *)(lVar7 + 0x28);
              iVar11 = 1;
              for (uVar10 = 1; (int)uVar10 < *(int *)(lVar7 + 0x18); uVar10 = uVar10 + 1) {
                if (*(uint *)(lVar7 + 0x18) <= uVar10) goto LAB_180e9ba8a;
                if (*plVar9 == 0) goto LAB_180e9ba84;
                puVar8 = (undefined8 *)FUN_183a14570(local_78,*plVar9,0);
                local_90 = *puVar8;
                uStack_80 = (undefined4)puVar8[2];
                uStack_7c = (undefined4)((ulonglong)puVar8[2] >> 0x20);
                uStack_88 = (undefined4)puVar8[1];
                uStack_84 = (undefined4)((ulonglong)puVar8[1] >> 0x20);
                local_a0 = uStack_7c;
                local_a8 = CONCAT44(uStack_80,uStack_84);
                fVar13 = (float)FUN_1804b5710(&local_a8,0);
                if (0.6 < ABS(fVar13 - fVar12)) break;
                iVar11 = iVar11 + 1;
                plVar9 = plVar9 + 1;
              }
              plVar9 = *(longlong **)(param_2 + 0x10);
              if (plVar9 != (longlong *)0x0) {
                uVar3 = (**(code **)(*plVar9 + 0x158))(plVar9,*(undefined8 *)(*plVar9 + 0x160));
                plVar9 = (longlong *)thunk_FUN_18039ec10(_DAT_184db8ae8);
                FUN_18318af80(plVar9,uVar3,0);
                if (plVar9 != (longlong *)0x0) {
                  uVar10 = (**(code **)(*plVar9 + 0x1a8))
                                     (plVar9,iVar11,*(undefined8 *)(*plVar9 + 0x1b0));
                  if (uVar10 < *(uint *)(lVar7 + 0x18)) {
                    return *(undefined8 *)(lVar7 + 0x20 + (longlong)(int)uVar10 * 8);
                  }
                  goto LAB_180e9ba8a;
                }
              }
            }
          }
        }
      }
    }
  }
LAB_180e9ba84:
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  uVar5 = (*pcVar1)();
  return uVar5;
}


