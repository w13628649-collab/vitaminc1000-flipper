/* REA ev_77566fb7d680d514d7c0ec8f57b221fba9e088470a46d63bcabf4a3d0778dff5; source response evidence/health_update_handler.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180bc2cb0(longlong *param_1,undefined8 param_2)

{
  undefined4 uVar1;
  undefined4 uVar2;
  undefined1 uVar3;
  undefined1 uVar4;
  short sVar5;
  undefined8 uVar6;
  undefined8 uVar7;
  code *UNRECOVERED_JUMPTABLE;
  longlong lVar8;
  undefined8 uVar9;
  
  if (DAT_18507bedd == '\0') {
    FUN_180361910(&DAT_184e54b70);
    FUN_180361910(&DAT_184dcaf78);
    DAT_18507bedd = '\x01';
  }
  lVar8 = FUN_182003eb0(param_2,_DAT_184dcaf78);
  if (lVar8 != 0) {
    uVar6 = *(undefined8 *)(lVar8 + 0x18);
    sVar5 = *(short *)(lVar8 + 0x38);
    uVar3 = *(undefined1 *)(lVar8 + 0x28);
    uVar4 = *(undefined1 *)(lVar8 + 0x29);
    uVar7 = *(undefined8 *)(lVar8 + 0x30);
    uVar1 = *(undefined4 *)(lVar8 + 0x24);
    uVar2 = *(undefined4 *)(lVar8 + 0x20);
    uVar9 = thunk_FUN_18039ec10(_DAT_184e54b70);
    FUN_1808f16b0(uVar9,uVar2,(int)sVar5,uVar3,uVar4,uVar7,uVar6,uVar1,0);
    UNRECOVERED_JUMPTABLE = *(code **)(*param_1 + 0x418);
                    /* WARNING: Could not recover jumptable at 0x000180bc2dd6. Too many branches */
                    /* WARNING: Treating indirect jump as call */
    (*UNRECOVERED_JUMPTABLE)(param_1,uVar9,*(undefined8 *)(*param_1 + 0x420),UNRECOVERED_JUMPTABLE);
    return;
  }
  FUN_180361b60();
  UNRECOVERED_JUMPTABLE = (code *)swi(3);
  (*UNRECOVERED_JUMPTABLE)();
  return;
}


