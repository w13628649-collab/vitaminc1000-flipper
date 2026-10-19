/* REA ev_b01227b0097253bf249ed53e19d8c29f07aa2df4ea54c15ce90636d2e8a6f29c; source response evidence/photon-event-code-wrapper.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180bc19b0(longlong param_1,undefined2 param_2,undefined8 param_3)

{
  code *pcVar1;
  
  if (DAT_18507bed7 == '\0') {
    FUN_180361910(&DAT_184d9d328);
    DAT_18507bed7 = '\x01';
  }
  if (*(longlong *)(param_1 + 0x160) != 0) {
    FUN_182ae4910(*(longlong *)(param_1 + 0x160),param_2,param_3,_DAT_184d9d328);
    return;
  }
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}


