/* REA ev_63b88a60dd86d13f0eab1e35b651715eee6ab6e231b059512fc60b044419233e; source response evidence/photon-operation-response-ingress.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180a8dc00(longlong param_1,longlong param_2)

{
  undefined4 uVar1;
  code *pcVar2;
  char cVar3;
  ushort *puVar4;
  longlong lVar5;
  longlong *plVar6;
  undefined8 *puVar7;
  undefined4 *puVar8;
  longlong *plVar9;
  ushort uVar10;
  longlong *plVar12;
  ushort uVar13;
  ushort uVar14;
  longlong local_res10 [2];
  longlong *local_res20;
  longlong *local_38 [2];
  ulonglong uVar11;
  
  if (DAT_18507b217 == '\0') {
    FUN_180361910(&DAT_184e054c8);
    FUN_180361910(&DAT_184e05628);
    FUN_180361910(&DAT_184daef70);
    FUN_180361910(&DAT_184daf0c8);
    FUN_180361910(&DAT_184de4720);
    DAT_18507b217 = '\x01';
  }
  plVar12 = (longlong *)0x0;
  local_res20 = (longlong *)0x0;
  local_38[0] = (longlong *)0x0;
  local_res10[0] = 0;
  if ((param_2 != 0) && (*(longlong *)(param_2 + 0x20) != 0)) {
    FUN_1832ad8d0(*(longlong *)(param_2 + 0x20),0xfd,&local_res20,0);
    uVar14 = 0;
    uVar13 = uVar14;
    if (local_res20 != (longlong *)0x0) {
      plVar9 = plVar12;
      if (*local_res20 == _DAT_185095f78) {
        plVar9 = local_res20;
      }
      if (plVar9 != (longlong *)0x0) {
        if (*(longlong *)(*local_res20 + 0x40) != *(longlong *)(_DAT_185095f78 + 0x40)) {
          FUN_180360c80(local_res20,_DAT_185095f78);
          pcVar2 = (code *)swi(3);
          (*pcVar2)();
          return;
        }
        puVar4 = (ushort *)il2cpp_object_unbox();
        uVar13 = *puVar4;
      }
    }
    lVar5 = *(longlong *)(param_1 + 0x120);
    plVar9 = plVar12;
    if (lVar5 != 0) {
      while ((int)plVar9 < *(int *)(lVar5 + 0x18)) {
        if ((*(longlong *)(param_1 + 0x120) == 0) ||
           (plVar6 = (longlong *)FUN_1821939c0(*(longlong *)(param_1 + 0x120),plVar9,_DAT_184daf0c8)
           , plVar6 == (longlong *)0x0)) goto LAB_180a8dfd7;
        lVar5 = *plVar6;
        uVar11 = 0;
        if (*(ushort *)(lVar5 + 0x12e) != 0) {
          do {
            if (*(longlong *)(*(longlong *)(lVar5 + 0xb0) + uVar11 * 0x10) == _DAT_184de4720) {
              puVar7 = (undefined8 *)
                       ((longlong)(*(int *)(*(longlong *)(lVar5 + 0xb0) + 8 + uVar11 * 0x10) + 3) *
                        0x10 + 0x138 + lVar5);
              goto LAB_180a8dd85;
            }
            uVar10 = (short)uVar11 + 1;
            uVar11 = (ulonglong)uVar10;
          } while (uVar10 < *(ushort *)(lVar5 + 0x12e));
        }
        puVar7 = (undefined8 *)FUN_18035ea20(plVar6,_DAT_184de4720,3);
LAB_180a8dd85:
        (*(code *)*puVar7)(plVar6,param_1,param_2,uVar13,puVar7[1]);
        lVar5 = *(longlong *)(param_1 + 0x120);
        plVar9 = (longlong *)(ulonglong)((int)plVar9 + 1);
        if (lVar5 == 0) goto LAB_180a8dfd7;
      }
      if (*(longlong *)(param_2 + 0x20) != 0) {
        cVar3 = FUN_1832ad8d0(*(longlong *)(param_2 + 0x20),0xff,local_38,0);
        if ((cVar3 != '\0') && (local_38[0] != (longlong *)0x0)) {
          plVar9 = plVar12;
          if (*local_38[0] == _DAT_185095f88) {
            plVar9 = local_38[0];
          }
          if (plVar9 != (longlong *)0x0) {
            if (*(longlong *)(*local_38[0] + 0x40) != *(longlong *)(_DAT_185095f88 + 0x40)) {
              FUN_180360c80(local_38[0],_DAT_185095f88);
              pcVar2 = (code *)swi(3);
              (*pcVar2)();
              return;
            }
            puVar8 = (undefined4 *)il2cpp_object_unbox();
            if (*(longlong *)(param_1 + 0x128) != 0) {
              uVar1 = *puVar8;
              FUN_182afe1d0(*(longlong *)(param_1 + 0x128),uVar1,local_res10,_DAT_184e05628);
              if (*(longlong *)(param_1 + 0x128) != 0) {
                FUN_182af6ad0(*(longlong *)(param_1 + 0x128),uVar1,_DAT_184e054c8);
                if (local_res10[0] == 0) goto LAB_180a8dedd;
                *(longlong *)(local_res10[0] + 0x18) = param_2;
                FUN_180360c40(local_res10[0] + 0x18,param_2);
                if ((local_res10[0] != 0) &&
                   (lVar5 = *(longlong *)(local_res10[0] + 0x10), lVar5 != 0)) {
                  (**(code **)(lVar5 + 0x18))
                            (*(undefined8 *)(lVar5 + 0x40),*(undefined8 *)(local_res10[0] + 0x18),
                             *(undefined8 *)(lVar5 + 0x28));
                  return;
                }
              }
            }
            goto LAB_180a8dfd7;
          }
        }
LAB_180a8dedd:
        lVar5 = *(longlong *)(param_1 + 0x120);
        goto joined_r0x000180a8deed;
      }
    }
  }
LAB_180a8dfd7:
  FUN_180361b60();
  pcVar2 = (code *)swi(3);
  (*pcVar2)();
  return;
joined_r0x000180a8deed:
  if (lVar5 == 0) goto LAB_180a8dfd7;
  if (*(int *)(lVar5 + 0x18) <= (int)plVar12) {
    return;
  }
  if ((*(longlong *)(param_1 + 0x120) == 0) ||
     (plVar9 = (longlong *)FUN_1821939c0(*(longlong *)(param_1 + 0x120),plVar12,_DAT_184daf0c8),
     plVar9 == (longlong *)0x0)) goto LAB_180a8dfd7;
  lVar5 = *plVar9;
  if (*(ushort *)(lVar5 + 0x12e) != 0) {
    uVar10 = uVar14;
    do {
      if (*(longlong *)(*(longlong *)(lVar5 + 0xb0) + (ulonglong)uVar10 * 0x10) == _DAT_184de4720) {
        puVar7 = (undefined8 *)
                 ((longlong)
                  (*(int *)(*(longlong *)(lVar5 + 0xb0) + 8 + (ulonglong)uVar10 * 0x10) + 4) * 0x10
                  + 0x138 + lVar5);
        goto LAB_180a8df75;
      }
      uVar10 = uVar10 + 1;
    } while (uVar10 < *(ushort *)(lVar5 + 0x12e));
  }
  puVar7 = (undefined8 *)FUN_18035ea20(plVar9,_DAT_184de4720,4);
LAB_180a8df75:
  (*(code *)*puVar7)(plVar9,param_1,param_2,uVar13,puVar7[1]);
  lVar5 = *(longlong *)(param_1 + 0x120);
  plVar12 = (longlong *)(ulonglong)((int)plVar12 + 1);
  goto joined_r0x000180a8deed;
}


