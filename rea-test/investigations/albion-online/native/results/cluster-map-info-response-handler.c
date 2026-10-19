/* REA ev_fa6bb27327d58d6be58cb7b58ad91541df1995056aa27601a65dfd57d744b96c; source response evidence/cluster-map-info-response-handler.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180a9c910(longlong param_1,longlong param_2)

{
  longlong lVar1;
  code *pcVar2;
  undefined8 uVar3;
  
  if (DAT_18507b413 == '\0') {
    FUN_180361910(&DAT_184db3e18);
    DAT_18507b413 = '\x01';
  }
  if (param_2 != 0) {
    if (*(short *)(param_2 + 0x12) == 0) {
      uVar3 = FUN_182004220(param_2,_DAT_184db3e18);
      lVar1 = *(longlong *)(param_1 + 0x10);
      if (lVar1 != 0) {
                    /* WARNING: Could not recover jumptable at 0x000180a9c979. Too many branches */
                    /* WARNING: Treating indirect jump as call */
        (**(code **)(lVar1 + 0x18))
                  (*(undefined8 *)(lVar1 + 0x40),uVar3,*(undefined8 *)(lVar1 + 0x28),
                   *(code **)(lVar1 + 0x18));
        return;
      }
    }
    else {
      lVar1 = *(longlong *)(param_1 + 0x10);
      if (lVar1 != 0) {
                    /* WARNING: Could not recover jumptable at 0x000180a9c99d. Too many branches */
                    /* WARNING: Treating indirect jump as call */
        (**(code **)(lVar1 + 0x18))(*(undefined8 *)(lVar1 + 0x40),0,*(undefined8 *)(lVar1 + 0x28));
        return;
      }
    }
  }
  FUN_180361b60();
  pcVar2 = (code *)swi(3);
  (*pcVar2)();
  return;
}


