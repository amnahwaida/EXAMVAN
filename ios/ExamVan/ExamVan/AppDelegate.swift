import UIKit

@main
class AppDelegate: UIResponder, UIApplicationDelegate {

    var window: UIWindow?

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        
        // Setup root VC for pre-iOS 13 if SceneDelegate isn't used
        if #available(iOS 13.0, *) {
            // Managed by SceneDelegate
        } else {
            let window = UIWindow(frame: UIScreen.main.bounds)
            let rootVC = ServerConfigViewController()
            let nav = UINavigationController(rootViewController: rootVC)
            window.rootViewController = nav
            window.makeKeyAndVisible()
            self.window = window
        }
        return true
    }

    // MARK: UISceneSession Lifecycle
    @available(iOS 13.0, *)
    func application(_ application: UIApplication, configurationForConnecting connectingSceneSession: UISceneSession, options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        return UISceneConfiguration(name: "Default Configuration", sessionRole: connectingSceneSession.role)
    }
}
