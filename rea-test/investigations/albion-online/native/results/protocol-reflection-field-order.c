/* REA ev_f7bd866e632a4d64320a6a27f4c8dfeb542f441f9c1169f19edba050b0bd6327; source response evidence/protocol-reflection-field-order.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

longlong FUN_18084da40(undefined8 param_1)

{
  longlong *plVar1;
  code *pcVar2;
  char cVar3;
  longlong lVar4;
  longlong lVar5;
  longlong *plVar6;
  undefined8 uVar7;
  undefined8 uVar8;
  longlong lVar9;
  longlong lVar10;
  longlong lVar11;
  longlong *plVar12;
  uint uVar13;
  undefined8 local_c8;
  undefined8 uStack_c0;
  longlong *local_b8;
  undefined8 local_b0;
  undefined8 uStack_a8;
  longlong *local_a0;
  undefined8 local_98;
  undefined8 uStack_90;
  longlong *local_88;
  undefined4 local_80;
  undefined4 uStack_7c;
  undefined4 uStack_78;
  undefined4 uStack_74;
  longlong *local_70;
  undefined4 local_68;
  undefined4 uStack_64;
  undefined4 uStack_60;
  undefined4 uStack_5c;
  longlong *local_58;
  
  if (DAT_18507a0d4 == '\0') {
    FUN_180361910(&DAT_184e37258);
    FUN_180361910(&DAT_184e37100);
    FUN_180361910(&DAT_184e7bd78);
    FUN_180361910(&DAT_184e4bd38);
    FUN_180361910(&DAT_184e57ea8);
    FUN_180361910(&DAT_184e59ee8);
    FUN_180361910(&DAT_184e5cc28);
    FUN_180361910(&DAT_184dfc818);
    FUN_180361910(&DAT_184dccf20);
    FUN_180361910(&DAT_184dcd078);
    FUN_180361910(&DAT_184dfc978);
    FUN_180361910(&DAT_184dcd1d0);
    FUN_180361910(&DAT_184dfcad8);
    FUN_180361910(&DAT_184dc9e38);
    FUN_180361910(&DAT_184dd2338);
    FUN_180361910(&DAT_184e70a50);
    FUN_180361910(&DAT_184db4f78);
    FUN_180361910(&DAT_184e70d00);
    FUN_180361910(&DAT_184e02de0);
    FUN_180361910(&DAT_184e02f40);
    FUN_180361910(&DAT_184db4e20);
    FUN_180361910(&DAT_184e707a0);
    FUN_180361910(&DAT_184e025a0);
    FUN_180361910(&DAT_184e53338);
    FUN_180361910(&DAT_184e68e90);
    FUN_180361910(&DAT_184d9e168);
    FUN_180361910(&DAT_184db9570);
    FUN_180361910(&DAT_184d97cb8);
    FUN_180361910(&DAT_184db96c8);
    FUN_180361910(&DAT_184d97e10);
    FUN_180361910(&DAT_184dda8b8);
    FUN_180361910(&DAT_184e00748);
    FUN_180361910(&DAT_184e761f0);
    DAT_18507a0d4 = '\x01';
  }
  local_b0 = 0;
  uStack_a8 = 0;
  local_a0 = (longlong *)0x0;
  local_98 = 0;
  uStack_90 = 0;
  local_88 = (longlong *)0x0;
  lVar4 = thunk_FUN_18039ec10(_DAT_184d97e10);
  FUN_1804873c0(lVar4,0);
  if (lVar4 != 0) {
    *(undefined8 *)(lVar4 + 0x10) = param_1;
    FUN_180360c40(lVar4 + 0x10,param_1);
    lVar5 = thunk_FUN_18039ec10(_DAT_184e7bd78);
    FUN_182b60120(lVar5,_DAT_184e37100);
    plVar6 = (longlong *)FUN_18308cad0(*(undefined8 *)(lVar4 + 0x10),0);
    if (plVar6 != (longlong *)0x0) {
      uVar7 = (**(code **)(*plVar6 + 0x248))(plVar6,*(undefined8 *)(*plVar6 + 0x250));
      uVar8 = thunk_FUN_18039ec10(_DAT_184dd2338);
      FUN_18243c590(uVar8,lVar4,_DAT_184db96c8,0);
      uVar7 = FUN_181e9cc20(uVar7,uVar8,_DAT_184e5cc28);
      lVar4 = FUN_181e9c4e0(uVar7,_DAT_184e59ee8);
      if (lVar4 != 0) {
        FUN_18218a540(&local_c8,lVar4,_DAT_184e02de0);
        local_68 = (undefined4)local_c8;
        uStack_64 = local_c8._4_4_;
        uStack_60 = (undefined4)uStack_c0;
        uStack_5c = uStack_c0._4_4_;
        local_58 = local_b8;
        do {
          cVar3 = FUN_182c89df0(&local_68,_DAT_184dfc978);
          plVar6 = local_58;
          if (cVar3 == '\0') {
            FUN_1804873c0(&local_68,_DAT_184dfc818);
            return lVar5;
          }
          lVar4 = thunk_FUN_18039ec10(_DAT_184e68e90);
          FUN_1821861f0(lVar4,_DAT_184e025a0);
          if (*(int *)(_DAT_184dda8b8 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          if (DAT_18507a0d3 == '\0') {
            FUN_180361910(&DAT_184e02860);
            FUN_180361910(&DAT_184dda8b8);
            DAT_18507a0d3 = '\x01';
          }
          if (plVar6 == (longlong *)0x0) goto LAB_18084e340;
          uVar7 = (**(code **)(*plVar6 + 0x888))(plVar6,*(undefined8 *)(*plVar6 + 0x890));
          if (*(int *)(_DAT_185096020 + 0xe4) == 0) {
            il2cpp_runtime_class_init();
          }
          cVar3 = FUN_1804322e0(uVar7,0,0);
          lVar9 = _DAT_184e02860;
          if (cVar3 == '\0') {
            if (lVar4 == 0) goto LAB_18084e33b;
          }
          else {
            if (lVar4 == 0) goto LAB_18084e2ec;
            *(int *)(lVar4 + 0x1c) = *(int *)(lVar4 + 0x1c) + 1;
            lVar10 = *(longlong *)(lVar4 + 0x10);
            uVar13 = *(uint *)(lVar4 + 0x18);
            if (lVar10 == 0) goto LAB_18084e2e7;
            if (uVar13 < *(uint *)(lVar10 + 0x18)) {
              *(uint *)(lVar4 + 0x18) = uVar13 + 1;
              if (*(uint *)(lVar10 + 0x18) <= uVar13) goto LAB_18084e2e1;
              *(longlong **)(lVar10 + 0x20 + (longlong)(int)uVar13 * 8) = plVar6;
              FUN_180360c40(lVar10 + 0x20 + (longlong)(int)uVar13 * 8,plVar6);
            }
            else {
              FUN_182252760(lVar4,plVar6,
                            *(undefined8 *)
                             (*(longlong *)(*(longlong *)(lVar9 + 0x20) + 0xc0) + 0x70));
            }
            uVar7 = (**(code **)(*plVar6 + 0x888))(plVar6,*(undefined8 *)(*plVar6 + 0x890));
            if (*(int *)(_DAT_184dda8b8 + 0xe4) == 0) {
              il2cpp_runtime_class_init();
            }
            FUN_18084bbe0(uVar7,lVar4,0);
          }
          FUN_182257ba0(lVar4,_DAT_184e02f40);
          lVar9 = thunk_FUN_18039ec10(_DAT_184d9e168);
          FUN_1821861f0(lVar9,_DAT_184db4e20);
          FUN_18218a540(&local_80,lVar4,_DAT_184e02de0);
          local_b0 = CONCAT44(uStack_7c,local_80);
          uStack_a8 = CONCAT44(uStack_74,uStack_78);
          local_a0 = local_70;
          uVar7 = 0;
          while( true ) {
            cVar3 = FUN_182c89df0(&local_b0,_DAT_184dfc978);
            plVar1 = local_a0;
            if (cVar3 == '\0') break;
            lVar4 = thunk_FUN_18039ec10(_DAT_184e53338);
            FUN_1821861f0(lVar4,_DAT_184e707a0);
            if (plVar1 == (longlong *)0x0) goto LAB_18084e32b;
            lVar10 = (**(code **)(*plVar1 + 0x6c8))(plVar1,0x16,*(undefined8 *)(*plVar1 + 0x6d0));
            uVar13 = 0;
            if (lVar10 == 0) goto LAB_18084e326;
            for (; (int)uVar13 < (int)*(uint *)(lVar10 + 0x18); uVar13 = uVar13 + 1) {
              if (*(uint *)(lVar10 + 0x18) <= uVar13) goto LAB_18084e2f2;
              plVar1 = *(longlong **)(lVar10 + 0x20 + (longlong)(int)uVar13 * 8);
              if (plVar1 == (longlong *)0x0) goto LAB_18084e310;
              lVar11 = (**(code **)(*plVar1 + 0x208))(plVar1,1,*(undefined8 *)(*plVar1 + 0x210));
              if (lVar11 == 0) goto LAB_18084e30b;
              if (*(longlong *)(lVar11 + 0x18) == 0) {
LAB_18084e057:
                if (lVar4 == 0) goto LAB_18084e306;
                FUN_180003680(lVar4,plVar1,_DAT_184e70a50);
              }
              else {
                if (*(int *)(lVar11 + 0x18) == 0) goto LAB_18084e2f7;
                if (*(longlong *)(lVar11 + 0x20) == 0) goto LAB_18084e301;
                plVar12 = (longlong *)FUN_1831cb6f0(*(longlong *)(lVar11 + 0x20),0);
                uVar8 = _DAT_184e761f0;
                if (*(int *)(_DAT_185096020 + 0xe4) == 0) {
                  il2cpp_runtime_class_init();
                }
                uVar8 = FUN_183196310(uVar8,0);
                if (plVar12 == (longlong *)0x0) goto LAB_18084e2fc;
                cVar3 = (**(code **)(*plVar12 + 0x968))
                                  (plVar12,uVar8,*(undefined8 *)(*plVar12 + 0x970));
                if (cVar3 == '\0') goto LAB_18084e057;
              }
            }
            if (*(int *)(_DAT_184d97cb8 + 0xe4) == 0) {
              il2cpp_runtime_class_init();
            }
            lVar10 = *(longlong *)(*(longlong *)(_DAT_184d97cb8 + 0xb8) + 8);
            if (lVar10 == 0) {
              if (*(int *)(_DAT_184d97cb8 + 0xe4) == 0) {
                il2cpp_runtime_class_init();
              }
              uVar8 = **(undefined8 **)(_DAT_184d97cb8 + 0xb8);
              lVar10 = thunk_FUN_18039ec10(_DAT_184dc9e38);
              FUN_182e75730(lVar10,uVar8,_DAT_184db9570,0);
              *(longlong *)(*(longlong *)(_DAT_184d97cb8 + 0xb8) + 8) = lVar10;
              FUN_180360c40(*(longlong *)(_DAT_184d97cb8 + 0xb8) + 8,lVar10);
            }
            uVar8 = FUN_181e93c40(lVar4,lVar10,_DAT_184e4bd38);
            lVar4 = FUN_181e9c4e0(uVar8,_DAT_184e57ea8);
            if (lVar4 == 0) goto LAB_18084e321;
            FUN_18218a540(&local_80,lVar4,_DAT_184e70d00);
            local_98 = CONCAT44(uStack_7c,local_80);
            uStack_90 = CONCAT44(uStack_74,uStack_78);
            local_88 = local_70;
            local_c8 = 0;
            uStack_c0 = &local_98;
            while( true ) {
              cVar3 = FUN_182c89df0(&local_98,_DAT_184dcd078);
              plVar1 = local_88;
              if (cVar3 == '\0') break;
              uVar8 = thunk_FUN_18039ec10(_DAT_184e00748);
              FUN_180747cb0(uVar8,plVar1,0);
              if (lVar9 == 0) goto LAB_18084e316;
              FUN_180003680(lVar9,uVar8);
            }
            FUN_1804873c0(&local_98,_DAT_184dccf20);
          }
          FUN_1804873c0(&local_b0,_DAT_184dfc818);
          if (lVar5 == 0) goto LAB_18084e336;
          FUN_182b8daf0(lVar5,plVar6,lVar9,_DAT_184e37258,uVar7);
        } while( true );
      }
    }
  }
  FUN_180361b60();
LAB_18084e2e1:
  FUN_180361b50();
LAB_18084e2e7:
  FUN_180361b60();
LAB_18084e2ec:
  FUN_180361b60();
LAB_18084e2f2:
  FUN_180361b50();
LAB_18084e2f7:
  FUN_180361b50();
LAB_18084e2fc:
  FUN_180361b60();
LAB_18084e301:
  FUN_180361b60();
LAB_18084e306:
  FUN_180361b60();
LAB_18084e30b:
  FUN_180361b60();
LAB_18084e310:
  FUN_180361b60();
LAB_18084e316:
  FUN_180361b60();
  FUN_18035da00();
LAB_18084e321:
  FUN_180361b60();
LAB_18084e326:
  FUN_180361b60();
LAB_18084e32b:
  FUN_180361b60();
  FUN_18035da00();
LAB_18084e336:
  FUN_180361b60();
LAB_18084e33b:
  FUN_180361b60();
LAB_18084e340:
  FUN_180361b60();
  FUN_18035da00();
  pcVar2 = (code *)swi(3);
  lVar4 = (*pcVar2)();
  return lVar4;
}


