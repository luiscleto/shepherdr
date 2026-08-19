export function returningToHome(previousPane: string | undefined, nextPane: string | undefined): boolean {
  return previousPane !== undefined && nextPane === undefined;
}
