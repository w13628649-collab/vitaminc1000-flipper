/* REA ev_69c27a66bb638371ce82cf0a1a6606a85e68720fbcee137980a92d6a5d5754c8; source response evidence/photon-event-ingress.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180a8d960(longlong param_1,longlong param_2)

{
  code *pcVar1;
  longlong *plVar2;
  undefined8 *puVar3;
  undefined2 *puVar4;
  longlong lVar5;
  ushort uVar6;
  undefined2 uVar8;
  int iVar9;
  ulonglong uVar7;
  
  if (DAT_18507b216 == '\0') {
    FUN_180361910(&DAT_184daef70);
    FUN_180361910(&DAT_184daf0c8);
    FUN_180361910(&DAT_184de4720);
    DAT_18507b216 = '\x01';
  }
  iVar9 = 0;
  if (param_2 != 0) {
    if (*(char *)(param_2 + 0x10) == '\x02') {
      iVar9 = 0;
      lVar5 = *(longlong *)(param_1 + 0x120);
      while (lVar5 != 0) {
        if (*(int *)(lVar5 + 0x18) <= iVar9) {
          return;
        }
        if ((*(longlong *)(param_1 + 0x120) == 0) ||
           (plVar2 = (longlong *)FUN_1821939c0(*(longlong *)(param_1 + 0x120),iVar9,_DAT_184daf0c8),
           plVar2 == (longlong *)0x0)) break;
        lVar5 = *plVar2;
        uVar7 = 0;
        if (*(ushort *)(lVar5 + 0x12e) != 0) {
          do {
            if (*(longlong *)(*(longlong *)(lVar5 + 0xb0) + uVar7 * 0x10) == _DAT_184de4720) {
              puVar3 = (undefined8 *)
                       ((longlong)(*(int *)(*(longlong *)(lVar5 + 0xb0) + 8 + uVar7 * 0x10) + 2) *
                        0x10 + 0x138 + lVar5);
              goto LAB_180a8da65;
            }
            uVar6 = (short)uVar7 + 1;
            uVar7 = (ulonglong)uVar6;
          } while (uVar6 < *(ushort *)(lVar5 + 0x12e));
        }
        puVar3 = (undefined8 *)FUN_18035ea20(plVar2,_DAT_184de4720,2);
LAB_180a8da65:
        (*(code *)*puVar3)(plVar2,param_1,param_2,puVar3[1]);
        iVar9 = iVar9 + 1;
        lVar5 = *(longlong *)(param_1 + 0x120);
      }
    }
    else {
      if (*(char *)(param_2 + 0x10) == '\x03') {
        uVar8 = 3;
      }
      else {
        if ((*(longlong *)(param_2 + 0x18) == 0) ||
           (plVar2 = (longlong *)FUN_1832ad840(*(longlong *)(param_2 + 0x18),0xfc,0),
           plVar2 == (longlong *)0x0)) goto LAB_180a8dbdf;
        if (*(longlong *)(*plVar2 + 0x40) != *(longlong *)(_DAT_185095f78 + 0x40)) {
          FUN_180360c80(plVar2,_DAT_185095f78);
LAB_180a8dbee:
          FUN_180361b60();
LAB_180a8dbf4:
          FUN_180361b60();
          pcVar1 = (code *)swi(3);
          (*pcVar1)();
          return;
        }
        puVar4 = (undefined2 *)il2cpp_object_unbox();
        uVar8 = *puVar4;
      }
      while (*(longlong *)(param_1 + 0x120) != 0) {
        if (*(int *)(*(longlong *)(param_1 + 0x120) + 0x18) <= iVar9) {
          return;
        }
        if (*(longlong *)(param_1 + 0x120) == 0) goto LAB_180a8dbf4;
        lVar5 = FUN_1821939c0(*(longlong *)(param_1 + 0x120),iVar9,_DAT_184daf0c8);
        if (lVar5 == 0) goto LAB_180a8dbee;
        FUN_18001a160(1,_DAT_184de4720,lVar5,param_1,param_2,uVar8);
        iVar9 = iVar9 + 1;
      }
    }
  }
LAB_180a8dbdf:
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}


